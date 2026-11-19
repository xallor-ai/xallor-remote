package cli

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xallor-ai/xallor-remote/internal/buildinfo"
	"github.com/xallor-ai/xallor-remote/internal/release"
	"github.com/xallor-ai/xallor-remote/internal/runtimeowner"
)

type nativeStopRefusal struct{ release.Control }

func (c nativeStopRefusal) PrepareStop(ctx context.Context, r release.StopRequest) (release.StopAttempt, error) {
	a, err := c.Control.PrepareStop(ctx, r)
	if err != nil {
		return a, err
	}
	return nativeRefusalAttempt{a}, nil
}

type nativeRefusalAttempt struct{ release.StopAttempt }

func (a nativeRefusalAttempt) Run(context.Context) (release.StopObservation, error) {
	i := a.Intent()
	o := release.StopObservation{AttemptID: i.AttemptID, Dispatch: "not_sent", Process: "still_running_or_unknown", RuntimeResource: "unknown", Jobs: "unreleased_or_unknown", Observers: "released", Result: "not_dispatched"}
	if i.Mode == "no_peer" {
		o.Process = "not_observed"
	}
	if _, err := a.Close(); err != nil {
		o.Observers = "unknown"
		return o, errors.Join(release.ErrCleanupUncertain, err)
	}
	return o, errors.New("fixture stop refused before dispatch")
}

type nativeJournalControl struct {
	runtimeControl
	probes    map[string]int
	t         *testing.T
	journal   runtimeowner.Journal
	operation string
	previous  *buildinfo.Executable
}

func (c *nativeJournalControl) PrepareProbe(r release.ProbeRequest) (release.ProbeAttempt, error) {
	p, e := c.runtimeControl.PrepareProbe(r)
	return &nativeJournalProbe{ProbeAttempt: p, c: c, r: r}, e
}

type nativeJournalProbe struct {
	release.ProbeAttempt
	c *nativeJournalControl
	r release.ProbeRequest
}

func (p *nativeJournalProbe) Run(ctx context.Context) (buildinfo.Executable, error) {
	rows, e := p.c.journal.Resources(p.c.operation)
	found := false
	for _, row := range rows {
		if row.ID == p.r.Resource.ID && row.Kind == runtimeowner.ResourceKindProbe {
			if row.AttemptID != p.r.Resource.AttemptID || row.Generation != p.r.Resource.Generation || row.ActivationID != filepath.Base(filepath.Dir(p.r.Binary)) || row.ImageSHA256 != p.r.Resource.ImageSHA256 || row.SourceSnapshotSHA256 != p.r.Resource.SourceSnapshotSHA256 || row.LaunchID != "" || row.Status != runtimeowner.ResourceAllocated {
				p.c.t.Fatal("probe intent identity mismatch", row, p.r)
			}
			found = true
		}
	}
	if e != nil || !found {
		p.c.t.Fatal("probe ran before durable obligation", rows, e)
	}
	raw, err := os.ReadFile(filepath.Join(filepath.Dir(p.r.Binary), "manifest.json"))
	manifest := sha256.Sum256(raw)
	snapshot := sha256.New()
	snapshot.Write(raw)
	snapshot.Write([]byte{0})
	snapshot.Write([]byte(p.r.Resource.ImageSHA256))
	if err != nil || p.r.ManifestSHA256 != hex.EncodeToString(manifest[:]) || p.r.Resource.SourceSnapshotSHA256 != hex.EncodeToString(snapshot.Sum(nil)) {
		p.c.t.Fatal("probe lost original manifest identity", p.r, err)
	}
	i, e := p.ProbeAttempt.Run(ctx)
	if e == nil {
		p.c.probes[p.r.Binary] = i.PID
	}
	return i, e
}

// Real Runtime processes and release controls exercise durable transaction
// recording. The verifier is the native test fixture, not signed-release proof;
// this test is not the not-yet-wired authenticated worker/IPC entry point.
func TestNativeJournalLifecycleRecordsUpgradeRollbackAndRestoration(t *testing.T) {
	profile, binary, first := nativeOwnerBundle(t)
	secondBundle, second := nativeCandidate(t, binary, first, "journal-second")
	failedBundle, failed := nativeCandidate(t, binary, first, "journal-failed")
	s, err := runtimeStore()
	if err != nil {
		t.Fatal(err)
	}
	j := runtimeowner.Journal{Dir: filepath.Join(s.Dir, "lifecycle")}
	defer func() {
		if err := stopNativeFixtureRuntime(); err != nil {
			t.Error("native fixture cleanup failed", err)
		}
	}()
	steps := []struct {
		kind, bundle string
		expected     release.Manifest
		failure      bool
		rejectStop   bool
	}{
		{"install", filepath.Dir(binary), first, false, false},
		{"install", secondBundle, second, false, false},
		{"rollback", "", first, false, false},
		{"install", secondBundle, first, true, true},
		{"install", failedBundle, first, true, false},
	}
	var identity string
	historical := map[string][]runtimeowner.ResourceRecord{}
	for i, step := range steps {
		baselineState, err := s.Read()
		if err != nil {
			t.Fatal(err)
		}
		baseline := release.ObserveSelection(baselineState)
		requested := step.expected
		manifestPath := filepath.Join(step.bundle, "manifest.json")
		if step.rejectStop {
			requested = second
		}
		if step.failure && !step.rejectStop {
			requested = failed
		}
		if step.kind == "rollback" {
			manifestPath = filepath.Join(filepath.Dir(binary), "manifest.json")
		}
		rawManifest, err := os.ReadFile(manifestPath)
		if err != nil {
			t.Fatal(err)
		}
		manifestDigest := sha256.Sum256(rawManifest)
		id := strings.Repeat(fmt.Sprintf("%x", i+1), 32)
		command := runtimeowner.Command{ID: id, Kind: step.kind, Bundle: step.bundle}
		request, err := command.Request()
		if err != nil {
			t.Fatal(err)
		}
		tx, err := j.Admit(request)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Close()
		if step.failure && !step.rejectStop {
			t.Setenv("XR_TEST_FAIL_CANDIDATE_SHA", failed.Artifact.SHA256)
		}
		previous, err := (runtimeControl{}).Running()
		if err != nil {
			t.Fatal(err)
		}
		trackedControl := &nativeJournalControl{probes: map[string]int{}, t: t, journal: j, operation: id, previous: previous}
		var control release.Control = trackedControl
		if step.rejectStop {
			control = nativeStopRefusal{Control: trackedControl}
		}
		err = tx.Run(command, s, control)
		operationErr := err
		if (err != nil) != step.failure {
			t.Fatalf("step %d unexpected lifecycle result: %v", i, err)
		}
		outcome := runtimeowner.Succeeded
		if step.failure {
			outcome = runtimeowner.Failed
		}
		if err := tx.Close(); err != nil {
			t.Fatal(err)
		}
		receipt, err := j.Query(id)
		if err != nil || receipt.Outcome != outcome {
			for path, pid := range trackedControl.probes {
				t.Logf("completed probe activation=%s pid=%d", filepath.Base(filepath.Dir(path)), pid)
			}
			diagnoseNativeActivationLocks(t, s.Dir)
			t.Fatal(receipt, err, operationErr)
		}
		n := len(receipt.Effects)
		binding := receipt.Binding
		if binding == nil || binding.ClearOnly || binding.Baseline != baseline || binding.Manifest != requested || binding.ManifestSHA256 != hex.EncodeToString(manifestDigest[:]) || binding.Requested.SHA256 != requested.Artifact.SHA256 || binding.Requested.ActivationID == "" || binding.Requested.LaunchID != "" {
			t.Fatal("receipt lost original resolved source or baseline", binding, baseline, requested)
		}
		if i == 0 && (baseline.Current.SHA256 != "" || baseline.Previous.SHA256 != "" || baseline.Pending.SHA256 != "") {
			t.Fatal("fresh install baseline is not empty", baseline)
		}
		if step.kind == "rollback" && binding.Requested.ActivationID != baseline.Previous.ActivationID {
			t.Fatal("rollback selected a different activation", binding)
		}
		purpose := "requested"
		if step.rejectStop {
			purpose = "cancel"
		} else if step.failure {
			purpose = "restore"
		}
		if n < 2 || receipt.Effects[n-2].Purpose != purpose || receipt.Effects[n-1].Purpose != purpose {
			t.Fatal("wrong terminal purpose", receipt)
		}
		if step.failure && binding.Requested.SHA256 == receipt.Effects[n-1].TargetSHA256 {
			t.Fatal("failed requested source replaced by restored baseline", receipt)
		}
		for _, effect := range receipt.Effects {
			if !effect.EffectIntent.Valid() || effect.Observed == nil || !effect.MatchesObserved(*effect.Observed) {
				t.Fatal("missing independently captured selection", effect)
			}
			if effect.Name == "start" && (!effect.Target.Complete() || effect.Target.LaunchID != effect.Before.Pending.LaunchID) {
				t.Fatal("start lost activation or pending launch identity", effect)
			}
		}
		selected, readErr := s.Read()
		resources, resourceErr := j.Resources(id)
		if resourceErr != nil || j.HasUnsettledResources(id) {
			t.Fatal("native lifecycle left resource debt", resources, resourceErr)
		}
		counts := map[string]int{}
		expectedObservers := 0
		for _, e := range receipt.Effects {
			if e.Name == "ready" || e.Name == "stop" {
				expectedObservers++
			}
		}
		for _, resource := range resources {
			counts[resource.Kind]++
			if resource.Kind == runtimeowner.ResourceKindActivation {
				if step.kind == "rollback" {
					t.Fatal("rollback invented new creation obligation", resource)
				}
				path := filepath.Join(s.Dir, "activations", resource.ActivationID)
				_, statErr := os.Stat(path)
				if step.failure {
					if resource.Status != runtimeowner.ResourceClosed || !os.IsNotExist(statErr) {
						t.Fatal("rejected candidate not released", resource, statErr)
					}
				} else if resource.Status != runtimeowner.ResourceRetained || statErr != nil || resource.LaunchID != selected.CurrentLaunchID || resource.ActivationID != selected.CurrentActivation {
					t.Fatal("candidate transfer differs from native selection", resource, statErr)
				}
			} else if resource.Status != runtimeowner.ResourceClosed {
				t.Fatal("native observer/source not durably closed", resource)
			}
		}
		if counts[runtimeowner.ResourceKindProbe] == 0 || counts[runtimeowner.ResourceKindSourceSnapshot] == 0 || counts[runtimeowner.ResourceKindObserver] != expectedObservers || (step.kind == "install" && counts[runtimeowner.ResourceKindActivation] != 1) {
			t.Fatal("missing native obligations", resources)
		}
		for oldID, oldRows := range historical {
			rows, err := j.Resources(oldID)
			if err != nil || fmt.Sprint(rows) != fmt.Sprint(oldRows) {
				t.Fatal("later operation rewrote historical creation debt", oldID, rows, err)
			}
		}
		historical[id] = resources
		if readErr != nil || n < 2 || receipt.Effects[n-1].Before != release.ObserveSelection(selected) || receipt.Effects[n-2].Target != receipt.Effects[n-1].Target {
			t.Fatal("receipt does not describe actual committed selection", receipt, readErr)
		}
		if n < 2 || receipt.Effects[n-2].Name != "commit" || receipt.Effects[n-1].Name != "ready" || receipt.Effects[n-1].TargetSHA256 != step.expected.Artifact.SHA256 {
			t.Fatal("missing final owner observation", receipt)
		}
		if step.failure && !step.rejectStop {
			seen := false
			for _, effect := range receipt.Effects {
				if effect.Name == "start" && effect.TargetSHA256 == failed.Artifact.SHA256 && effect.State == runtimeowner.EffectQuiescentFailure {
					seen = true
				}
			}
			if !seen {
				t.Fatal("failed process was recorded as never executed", receipt)
			}
		}
		if step.rejectStop {
			seen := false
			for _, effect := range receipt.Effects {
				if effect.Name == "start" {
					t.Fatal("started after stop refusal")
				}
				if effect.Name == "stop" && effect.State == runtimeowner.EffectNotApplied {
					seen = true
				}
			}
			if !seen {
				t.Fatal("stop refusal was not recorded")
			}
		}
		live, err := (runtimeControl{}).Running()
		if err != nil || live == nil || live.SHA256 != step.expected.Artifact.SHA256 {
			t.Fatal("actual runtime differs from receipt", live, err)
		}
		digest := nativeIdentityDigest(t, profile)
		if identity == "" {
			identity = digest
		} else if identity != digest {
			t.Fatal("lifecycle changed device identity")
		}
	}
}

// A real prepared peer is durably identified before its same-connection dispatch.
func (c *nativeJournalControl) PrepareStop(ctx context.Context, r release.StopRequest) (release.StopAttempt, error) {
	rows, readErr := c.journal.Resources(c.operation)
	found := false
	for _, row := range rows {
		if row.ResourceObligation == runtimeowner.ResourceObligation(r.Observer) && row.Kind == runtimeowner.ResourceKindObserver && row.Status == runtimeowner.ResourceAllocated {
			found = true
		}
	}
	if readErr != nil || !found {
		c.t.Fatal("stop preparation started without durable observer obligation", rows, readErr)
	}
	a, err := c.runtimeControl.PrepareStop(ctx, r)
	if err != nil {
		return a, err
	}
	intent := a.Intent()
	if intent.AttemptID != r.Observer.AttemptID {
		c.t.Fatal("stop attempt detached from preparation group", intent, r)
	}
	if c.previous != nil {
		p := c.previous
		if intent.PeerPID != p.PID || intent.ImageSHA256 != p.SHA256 || intent.Selection.ActivationID != p.ActivationID || intent.Selection.LaunchID != p.LaunchID {
			_, _ = a.Close()
			c.t.Fatal("prepared stop differs from actual old peer", intent, p)
		}
	}
	return &nativeJournalStop{StopAttempt: a, control: c}, nil
}

type nativeJournalStop struct {
	release.StopAttempt
	control *nativeJournalControl
}

func (a *nativeJournalStop) Run(ctx context.Context) (release.StopObservation, error) {
	c := a.control
	r, err := c.journal.Query(c.operation)
	if err != nil || len(r.Effects) == 0 {
		c.t.Fatal("missing stop intent before dispatch", r, err)
	}
	last := r.Effects[len(r.Effects)-1]
	if last.State != runtimeowner.EffectPending || last.Stop == nil || *last.Stop != a.Intent() || last.StopObserved != nil {
		c.t.Fatal("stop not durably prepared", last)
	}
	o, err := a.StopAttempt.Run(ctx)
	if err == nil && (o.Observers != "released" || !o.Confirms(a.Intent())) {
		c.t.Fatal("native observers not released before confirmation", o)
	}
	return o, err
}
