package release

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// Controlled provenance exercises real installation and disk staging; it is not
// signed native acceptance. Every fresh candidate needs a creation obligation.
func TestTrackedInstall_shouldRecordActivationBeforeSelection(t *testing.T) {
	bundle, m := bundle(t, "tracked-activation")
	ledger := &resourceTrace{}
	s := mechanicsStore(t).WithResources(ledger)
	if err := s.Install(bundle, control(m)); err != nil {
		t.Fatal(err)
	}
	state, err := s.Read()
	if err != nil {
		t.Fatal(err)
	}
	for _, obligation := range ledger.intent {
		if obligation.Kind == "activation" {
			if obligation.ActivationID != state.CurrentActivation || obligation.LaunchID != state.CurrentLaunchID || obligation.SourceSnapshotSHA256 == "" {
				t.Fatalf("creation identity changed: %+v", obligation)
			}
			return
		}
	}
	t.Fatal("successful tracked install allocated candidate without durable activation obligation")
}

// Fresh bootstrap uses its creation launch; this fixture only observes identity
// and does not introduce a production tracked-bootstrap transaction contract.
func TestFreshBootstrap_shouldUsePreallocatedCreationLaunch(t *testing.T) {
	dir, m := bundle(t, "bootstrap-launch")
	s := mechanicsStore(t)
	ledger := &resourceTrace{}
	if err := s.WithResources(ledger).EnsureRunning(dir, control(m)); err != nil {
		t.Fatal(err)
	}
	state, err := s.Read()
	if err != nil {
		t.Fatal(err)
	}
	for _, i := range ledger.intent {
		if i.Kind == "activation" {
			if i.LaunchID != state.CurrentLaunchID {
				t.Fatal("bootstrap replaced creation launch", i.LaunchID, state.CurrentLaunchID)
			}
			return
		}
	}
	t.Fatal("missing creation identity")
}

type activationRefusalLedger struct {
	resourceTrace
	store Store
	t     *testing.T
}

func (r *activationRefusalLedger) RecordIntent(i ResourceObligation) error {
	if i.Kind == "activation" {
		if _, err := os.Stat(filepath.Join(r.store.Dir, "activations", i.ActivationID)); !os.IsNotExist(err) {
			r.t.Fatal("allocated before intent", err)
		}
		return errors.New("intent rejected")
	}
	return r.resourceTrace.RecordIntent(i)
}

// Failed persistence cannot allocate the candidate or dispatch a start.
func TestTrackedInstall_shouldNotAllocate_whenActivationIntentRejected(t *testing.T) {
	dir, m := bundle(t, "activation-intent-refused")
	s := mechanicsStore(t)
	r := &activationRefusalLedger{store: s, t: t}
	c := control(m)
	if err := s.WithResources(r).Install(dir, c); !errors.Is(err, ErrCleanupUncertain) {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(filepath.Join(s.Dir, "activations"))
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if len(entries) != 0 || c.live != nil {
		t.Fatal("candidate allocated after refused intent", entries, c.live)
	}
}

type transferFailureLedger struct{ resourceTrace }

func (r *transferFailureLedger) RetainSelected(ResourceObligation, ReadyObservation) error {
	return errors.New("transfer write failed")
}

// Once commit and ready succeeded, transfer failure keeps the selected native
// candidate and unresolved obligation instead of restoring or deleting it.
func TestTrackedInstall_shouldPreserveSelectedCandidate_whenTransferFails(t *testing.T) {
	dir, m := bundle(t, "transfer-refused")
	s := mechanicsStore(t)
	r := &transferFailureLedger{}
	c := control(m)
	if err := s.WithResources(r).Install(dir, c); !errors.Is(err, ErrCleanupUncertain) {
		t.Fatal(err)
	}
	selected, err := s.Read()
	if err != nil || selected.Current == nil || c.live == nil {
		t.Fatal(selected, err)
	}
	if _, err := os.Stat(filepath.Join(s.Dir, "activations", selected.CurrentActivation)); err != nil {
		t.Fatal("selected candidate deleted", err)
	}
	if len(r.unknown) != 1 || r.unknown[0].Kind != "activation" {
		t.Fatal("missing activation debt", r)
	}
}

// Deterministic same-version refusal removes only its newly created receipt;
// the original selected activation remains present and its debt is untouched.
func TestTrackedInstall_shouldCloseRejectedCandidate_whenSameVersionSelected(t *testing.T) {
	dir, m := bundle(t, "same-version")
	s := mechanicsStore(t)
	c := control(m)
	if err := s.Install(dir, c); err != nil {
		t.Fatal(err)
	}
	baseline, _ := s.Read()
	r := &resourceTrace{}
	if err := s.WithResources(r).Install(dir, c); err == nil {
		t.Fatal("same version accepted")
	}
	var candidate ResourceObligation
	for _, i := range r.intent {
		if i.Kind == "activation" {
			candidate = i
		}
	}
	closed := false
	for _, i := range r.closed {
		closed = closed || i == candidate
	}
	if candidate.ID == "" || !closed {
		t.Fatal("candidate debt not closed", r)
	}
	if _, err := os.Stat(filepath.Join(s.Dir, "activations", candidate.ActivationID)); !os.IsNotExist(err) {
		t.Fatal("rejected candidate retained", err)
	}
	if _, err := os.Stat(filepath.Join(s.Dir, "activations", baseline.CurrentActivation)); err != nil {
		t.Fatal("baseline changed", err)
	}
}
