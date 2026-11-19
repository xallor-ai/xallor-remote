package release

import (
	"context"
	"errors"
	"github.com/xallor-ai/xallor-remote/internal/buildinfo"
	"testing"
)

type probeFaultControl struct {
	*fakeControl
	mode string
	runs int
}
type probeFaultAttempt struct {
	c *probeFaultControl
	r ProbeRequest
}

func (c *probeFaultControl) PrepareProbe(r ProbeRequest) (ProbeAttempt, error) {
	if c.mode == "missing-attempt" {
		return nil, errors.New("partial prepare")
	}
	p := &probeFaultAttempt{c: c, r: r}
	if c.mode == "partial-prepare" {
		return p, errors.New("partial prepare")
	}
	return p, nil
}
func (p *probeFaultAttempt) Run(context.Context) (buildinfo.Executable, error) {
	p.c.runs++
	return p.c.check(p.r.Binary, p.r.Resource.ImageSHA256)
}
func (p *probeFaultAttempt) Close() ([]ProbeClosure, error) {
	r := ProbeClosure{Resource: p.r.Resource, Closed: true}
	switch p.c.mode {
	case "missing":
		return nil, nil
	case "duplicate":
		return []ProbeClosure{r, r}, nil
	case "wrong":
		r.Resource.AttemptID = "wrong"
	case "partial-prepare", "unclosed":
		r.Closed = false
	case "close-error":
		return []ProbeClosure{r}, errors.New("native close failed")
	}
	return []ProbeClosure{r}, nil
}

type probeFaultLedger struct {
	resourceTrace
	mode string
}

func (r *probeFaultLedger) RecordIntent(i ResourceObligation) error {
	if i.Kind == "probe" && r.mode == "intent" {
		return errors.New("intent unavailable")
	}
	return r.resourceTrace.RecordIntent(i)
}
func (r *probeFaultLedger) Close(i ResourceObligation) error {
	if i.Kind == "probe" && r.mode == "durable-close" {
		return errors.New("closure unavailable")
	}
	return r.resourceTrace.Close(i)
}

// Any incomplete native receipt or failed durable closure must stop before
// pending selection and Stop. Exact original obligations remain unresolved.
func TestProbeLedger_shouldBlockLifecycle_whenCleanupIsUnproven(t *testing.T) {
	for _, mode := range []string{"intent", "missing-attempt", "partial-prepare", "missing", "duplicate", "wrong", "unclosed", "close-error", "durable-close"} {
		t.Run(mode, func(t *testing.T) {
			dir, m := bundle(t, "probe-"+mode)
			s := mechanicsStore(t)
			c := &probeFaultControl{fakeControl: control(m), mode: mode}
			r := &probeFaultLedger{mode: mode}
			err := s.WithResources(r).Install(dir, c)
			if !errors.Is(err, ErrCleanupUncertain) || c.stops != 0 || c.live != nil {
				t.Fatal("uncertain probe permitted lifecycle", err, c.stops, c.live)
			}
			state, e := s.Read()
			if e != nil || state.Pending != nil || state.Current != nil {
				t.Fatal(state, e)
			}
			if (mode == "intent" || mode == "partial-prepare" || mode == "missing-attempt") && c.runs != 0 {
				t.Fatal("run before successful preparation/intent")
			}
			for _, closed := range r.closed {
				if closed.Kind == "probe" {
					t.Fatal("unproven receipt closed", closed)
				}
			}
			if mode != "intent" && mode != "durable-close" {
				found := false
				for _, u := range r.unknown {
					if u.Kind == "probe" {
						found = true
						matched := false
						for _, i := range r.intent {
							if u == i {
								matched = true
							}
						}
						if !matched {
							t.Fatal("unknown wrong resource", u)
						}
					}
				}
				if !found {
					t.Fatal("missing probe debt")
				}
			}
		})
	}
}

// A successful probe binds the original authenticated snapshot, carries no
// fabricated service launch, and closes exactly the resource it created.
func TestProbeLedger_shouldBindOriginalSnapshotAndCloseExactReceipt(t *testing.T) {
	dir, m := bundle(t, "probe-identity")
	r := &resourceTrace{}
	s := mechanicsStore(t).WithResources(r)
	if err := s.Install(dir, control(m)); err != nil {
		t.Fatal(err)
	}
	probes := 0
	for _, i := range r.intent {
		if i.Kind != "probe" {
			continue
		}
		probes++
		if i.LaunchID != "" || i.ActivationID == "" || i.Phase != "preflight" {
			t.Fatal(i)
		}
		matched := false
		for _, source := range r.intent {
			if source.Kind == "source_snapshot" && source.SourceSnapshotSHA256 == i.SourceSnapshotSHA256 && source.ImageSHA256 == i.ImageSHA256 {
				matched = true
			}
		}
		if !matched {
			t.Fatal("probe drifted from source snapshot", i)
		}
		closes := 0
		for _, c := range r.closed {
			if c == i {
				closes++
			}
		}
		if closes != 1 {
			t.Fatal("wrong closure count", closes)
		}
	}
	if probes == 0 {
		t.Fatal("no probes")
	}
}
