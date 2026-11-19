//go:build windows

package release

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/xallor-ai/xallor-remote/internal/runtimeimage"
	"golang.org/x/sys/windows"
)

// A real deny-delete handle simulates transient file inspection. After the
// first explicitly refused delete releases it, retry must recheck ownership.
func TestActivationRemovalRetriesDeniedDeleteAfterOwnershipRecheck(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "image.exe")
	raw := []byte("owned fixture")
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(raw)
	held, err := runtimeimage.Open(path, hex.EncodeToString(digest[:]))
	if err != nil {
		t.Fatal(err)
	}
	defer held.Close()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	checks, calls := 0, 0
	check := func() error {
		checks++
		now, err := root.Stat("image.exe")
		if err != nil {
			return err
		}
		if !os.SameFile(before, now) {
			return errors.New("ownership changed")
		}
		return nil
	}
	remove := func() error {
		calls++
		err := root.Remove("image.exe")
		if calls == 1 {
			if err == nil {
				t.Fatal("expected real deny-delete failure")
			}
			held.Close()
		}
		return err
	}
	if err := removeActivationFile(check, remove); err != nil {
		t.Fatal(err)
	}
	if calls != 2 || checks != 2 {
		t.Fatal("retry skipped ownership recheck", calls, checks)
	}
}

// A replacement between attempts is never removed, even after the same denial.
func TestActivationRemovalRefusesChangedOwnershipBeforeRetry(t *testing.T) {
	changed := errors.New("ownership changed")
	checks, calls := 0, 0
	err := removeActivationFile(func() error {
		checks++
		if checks > 1 {
			return changed
		}
		return nil
	}, func() error { calls++; return windows.ERROR_ACCESS_DENIED })
	if !errors.Is(err, changed) || calls != 1 {
		t.Fatal("retry ignored changed ownership", calls, checks, err)
	}
}

// Persistent denial has a finite observation budget and preserves the failure;
// unrelated errors are never automatically repeated.
func TestActivationRemovalStopsOnPersistentDenialOrUnknownFailure(t *testing.T) {
	started := time.Now()
	calls := 0
	err := removeActivationFile(func() error { return nil }, func() error { calls++; return windows.ERROR_SHARING_VIOLATION })
	if !errors.Is(err, windows.ERROR_SHARING_VIOLATION) || calls < 2 || time.Since(started) > 3*time.Second {
		t.Fatal(calls, time.Since(started), err)
	}
	calls = 0
	unknown := errors.New("unknown remove result")
	err = removeActivationFile(func() error { return nil }, func() error { calls++; return unknown })
	if !errors.Is(err, unknown) || calls != 1 {
		t.Fatal(calls, err)
	}
}
