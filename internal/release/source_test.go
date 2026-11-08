package release

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xallor-ai/xallor-remote/internal/releasetrust"
)

func sourceBundle(t *testing.T, label string) (string, Manifest) {
	t.Helper()
	dir, m := bundle(t, label)
	m.Build.Revision = strings.Repeat("a", 40)
	writeManifest(t, dir, m)
	return dir, m
}

// The standalone entry defaults to refusing unsigned development metadata;
// this is not an install/upgrade/rollback authentication test.
func TestSourceVerify_should_refuseDevelopmentBuild_withoutReturningSuccess(t *testing.T) {
	dir, m := sourceBundle(t, "development")
	m.Build.Dirty = true
	writeManifest(t, dir, m)
	result, err := VerifyBundleSource(context.Background(), dir, "")
	if !errors.Is(err, releasetrust.ErrUnverified) || result.Verified {
		t.Fatal(result, err)
	}
}

// A verifier refusal prevents snapshot consumption despite a recomputed hash.
// Injected refusal checks ordering only, not Sigstore cryptographic correctness.
func TestSourceSnapshot_should_refuseConsumption_whenRehashedBytesAreUnverified(t *testing.T) {
	dir, m := sourceBundle(t, "untrusted")
	data := []byte("modified-and-rehashed")
	sum := sha256.Sum256(data)
	m.Artifact.SHA256 = hex.EncodeToString(sum[:])
	m.Artifact.Size = int64(len(data))
	if err := os.WriteFile(filepath.Join(dir, m.Artifact.Filename), data, 0700); err != nil {
		t.Fatal(err)
	}
	writeManifest(t, dir, m)
	consumed := false
	deny := func(context.Context, string, releasetrust.Identity, string, string) error {
		return releasetrust.ErrUnverified
	}
	_, err := withSourceSnapshot(context.Background(), dir, "", deny, func(string, Manifest, []byte) error { consumed = true; return nil })
	if !errors.Is(err, releasetrust.ErrUnverified) || consumed {
		t.Fatal("unverified snapshot consumed", err)
	}
}

// Snapshot isolation is independent of authentication. The injected acceptor
// permits observing which bytes the helper exposes, not certifying those bytes.
func TestSourceSnapshot_should_preserveBytes_whenInputChangesDuringVerification(t *testing.T) {
	dir, m := sourceBundle(t, "stable")
	verifier := func(context.Context, string, releasetrust.Identity, string, string) error {
		return os.WriteFile(filepath.Join(dir, m.Artifact.Filename), []byte("changed-after-snapshot"), 0700)
	}
	called := false
	_, err := withSourceSnapshot(context.Background(), dir, "", verifier, func(binary string, got Manifest, _ []byte) error {
		called = true
		data, err := os.ReadFile(binary)
		if err != nil {
			return err
		}
		if got != m || string(data) != "fake-test-artifact-stable" {
			t.Fatal("mutable input replaced snapshot")
		}
		return nil
	})
	if err != nil || !called {
		t.Fatal(err)
	}
}
