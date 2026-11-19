package release

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xallor-ai/xallor-remote/internal/buildinfo"
)

// Explicit recovery runs the retained current baseline, or the sole pending
// image for a failed first bootstrap; it never selects a version-cache fallback.
func TestRecoveryHandoffBindsRetainedSourceAndExactPendingState(t *testing.T) {
	for _, hasCurrent := range []bool{false, true} {
		s, a := stagedCleanupFixture(t)
		state := State{Pending: &a.Manifest, PendingActivation: a.ID}
		if hasCurrent {
			state.Current = &a.Manifest
			state.CurrentActivation = a.ID
		}
		if err := s.save(state); err != nil {
			t.Fatal(err)
		}
		id, hash := strings.Repeat("1", 32), strings.Repeat("b", 64)
		permit, activation, err := s.NewRecoveryHandoff(context.Background(), 100, id, hash)
		if err != nil {
			t.Fatal(err)
		}
		worker := buildinfo.Executable{Info: a.Manifest.Build, Path: a.Binary, SHA256: a.Manifest.Artifact.SHA256, PID: 101, OwnerProtocol: buildinfo.RuntimeOwnerProtocol}
		if activation.ID != a.ID {
			t.Fatal("unexpected recovery selection")
		}
		if err := s.AuthorizeRecoveryHandoff(context.Background(), permit, worker, 100, id, hash); err != nil {
			t.Fatal(err)
		}
		if err := s.save(State{}); err != nil {
			t.Fatal(err)
		}
		if err := s.AuthorizeRecoveryHandoff(context.Background(), permit, worker, 100, id, hash); err == nil {
			t.Fatal("stale recovery context accepted")
		}
	}
}

// Missing original proof or a different worker/operation cannot enter recovery;
// constructing a recovery permit never writes selection or initializes identity.
func TestRecoveryHandoffRefusesLostProofAndMismatchedWorker(t *testing.T) {
	s, a := stagedCleanupFixture(t)
	if err := s.save(State{Current: &a.Manifest, CurrentActivation: a.ID, Pending: &a.Manifest, PendingActivation: a.ID}); err != nil {
		t.Fatal(err)
	}
	id, hash := strings.Repeat("1", 32), strings.Repeat("b", 64)
	p, _, err := s.NewRecoveryHandoff(context.Background(), 100, id, hash)
	if err != nil {
		t.Fatal(err)
	}
	worker := buildinfo.Executable{Info: a.Manifest.Build, Path: a.Binary, SHA256: strings.Repeat("c", 64), PID: 101, OwnerProtocol: buildinfo.RuntimeOwnerProtocol}
	if err := s.AuthorizeRecoveryHandoff(context.Background(), p, worker, 100, id, hash); err == nil {
		t.Fatal("other worker accepted")
	}
	if err := os.Remove(filepath.Join(a.Dir, "manifest.json")); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.NewRecoveryHandoff(context.Background(), 100, id, hash); err == nil {
		t.Fatal("missing original proof accepted")
	}
}
