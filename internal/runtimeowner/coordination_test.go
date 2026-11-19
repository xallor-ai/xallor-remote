package runtimeowner

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xallor-ai/xallor-remote/internal/release"
)

// The worker derives its own request digest from validated command fields;
// the head cannot provide a digest independent of the bundle actually installed.
func TestCommandBindsIntentAndRejectsAmbiguousInput(t *testing.T) {
	a := Command{ID: strings.Repeat("1", 32), Kind: "install", Bundle: filepath.Join(t.TempDir(), "bundle")}
	r, err := a.Request()
	if err != nil {
		t.Fatal(err)
	}
	b := a
	b.Bundle += "-other"
	other, err := b.Request()
	if err != nil || other.IntentSHA256 == r.IntentSHA256 {
		t.Fatal(other, err)
	}
	b = a
	b.Kind = "rollback"
	if _, err := b.Request(); err == nil {
		t.Fatal("ignored rollback bundle")
	}
	b = a
	b.Bundle = "relative"
	if _, err := b.Request(); err == nil {
		t.Fatal("relative worker input")
	}
}

// Omitted install input cannot silently become the launcher's working directory.
func TestNewCommandRequiresExplicitInstallBundle(t *testing.T) {
	if _, err := NewCommand("install", ""); !errors.Is(err, ErrInvalid) {
		t.Fatal("missing bundle became implicit cwd", err)
	}
}

// Uncertainty before a recorded effect must still reserve admission. Merely
// seeing an empty effect list cannot turn failed cleanup into a rejection.
func TestCompletePreservesPreEffectUncertainty(t *testing.T) {
	j := journal(t)
	tx, err := j.Admit(request("1"))
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.Complete(release.ErrCleanupUncertain); !errors.Is(err, ErrUncertain) {
		t.Fatal(err)
	}
	tx.Close()
	r, err := j.Active()
	if err != nil || r.Outcome != Uncertain {
		t.Fatal(r, err)
	}
	if _, err := j.Admit(request("2")); !errors.Is(err, ErrPending) {
		t.Fatal(err)
	}
}

// A deterministic preflight rejection records a terminal receipt; observing an
// absent journal remains read-only and a new ID can be admitted after rejection.
func TestCompleteRejectionAndReadOnlyActiveObservation(t *testing.T) {
	j := journal(t)
	if _, err := j.Active(); !errors.Is(err, ErrUnknown) {
		t.Fatal(err)
	}
	if _, err := os.Stat(j.Dir); !os.IsNotExist(err) {
		t.Fatal("observation created directory")
	}
	tx, err := j.Admit(request("1"))
	if err != nil {
		t.Fatal(err)
	}
	cause := errors.New("candidate denied before effects")
	if err := tx.Complete(cause); !errors.Is(err, cause) {
		t.Fatal(err)
	}
	tx.Close()
	if _, err := j.Active(); !errors.Is(err, ErrUnknown) {
		t.Fatal(err)
	}
	r, err := j.Query(request("1").ID)
	if err != nil || r.Outcome != Rejected {
		t.Fatal(r, err)
	}
	next, err := j.Admit(request("2"))
	if err != nil {
		t.Fatal(err)
	}
	next.Close()
}

// A nil backend error without commit/ready evidence is not success.
func TestCompleteCannotInventSuccessfulActivation(t *testing.T) {
	j := journal(t)
	tx, err := j.Admit(request("1"))
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Close()
	if err := tx.Complete(nil); !errors.Is(err, ErrUncertain) {
		t.Fatal(err)
	}
	r, err := j.Query(request("1").ID)
	if err != nil || r.Outcome != Uncertain {
		t.Fatal(r, err)
	}
}

// Once Run owns completion, disconnect/cancellation callbacks cannot publish
// a terminal receipt while its preflight or backend is still running.
func TestExternalCompletionCannotFinalizeClaimedRunner(t *testing.T) {
	j := journal(t)
	tx, err := j.Admit(request("1"))
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Close()
	tx.running = true
	if err := tx.Complete(context.Canceled); !errors.Is(err, ErrRecorded) {
		t.Fatal(err)
	}
	if err := tx.Finish(Rejected); !errors.Is(err, ErrRecorded) {
		t.Fatal(err)
	}
	r, err := j.Query(request("1").ID)
	if err != nil || r.Outcome != Pending {
		t.Fatal("external callback completed active runner", r, err)
	}
}
