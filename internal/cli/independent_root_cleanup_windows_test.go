package cli

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

func retryableIndependentRootRemove(err error) bool {
	return errors.Is(err, windows.ERROR_ACCESS_DENIED) || errors.Is(err, windows.ERROR_SHARING_VIOLATION)
}

func ownedCleanupFixture(t *testing.T) (string, os.FileInfo) {
	t.Helper()
	root, err := os.MkdirTemp("", "xr-ipc-cleanup-")
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(root) != filepath.Clean(os.TempDir()) {
		t.Fatal("unexpected test root")
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(root); err != nil {
			t.Error(err)
		}
	})
	info, err := os.Stat(root)
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(info, info) {
		t.Fatal("root identity not captured")
	}
	return root, info
}

// A real protected-image handle denies the first deletion. After that refusal
// releases the known handle, cleanup must retry the same owned root, not fail.
func TestIndependentRootCleanup_shouldRetryAfterTransientImageDenial(t *testing.T) {
	root, info := ownedCleanupFixture(t)
	path := filepath.Join(root, "image.exe")
	raw := []byte("controlled image bytes")
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(raw)
	held, err := runtimeimage.Open(path, hex.EncodeToString(digest[:]))
	if err != nil {
		t.Fatal(err)
	}
	defer held.Close()
	calls := 0
	err = removeIndependentRootUsing(root, info, func(path string) error {
		calls++
		err := os.RemoveAll(path)
		if calls == 1 {
			if !errors.Is(err, windows.ERROR_ACCESS_DENIED) && !errors.Is(err, windows.ERROR_SHARING_VIOLATION) {
				t.Fatal("expected real image denial", err)
			}
			if err := held.Close(); err != nil {
				t.Fatal(err)
			}
		}
		return err
	})
	if err != nil || calls != 2 {
		t.Fatal("transient denial not settled", calls, err)
	}
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatal("root remains", err)
	}
}

// The directory at the same pathname can change between attempts. Revalidation
// must refuse its replacement, even if the previous error was retryable.
func TestIndependentRootCleanup_shouldRefuseReplacementBeforeRetry(t *testing.T) {
	root, info := ownedCleanupFixture(t)
	calls := 0
	err := removeIndependentRootUsing(root, info, func(path string) error {
		calls++
		if err := os.Rename(path, path+"-original"); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.RemoveAll(path + "-original") })
		if err := os.Mkdir(path, 0700); err != nil {
			t.Fatal(err)
		}
		return windows.ERROR_ACCESS_DENIED
	})
	if err == nil || calls != 1 || errors.Is(err, windows.ERROR_ACCESS_DENIED) {
		t.Fatal("replacement root was not refused by identity", calls, err)
	}
	if _, err := os.Stat(root); err != nil {
		t.Fatal("replacement removed", err)
	}
}

// Persistent access denial remains failure; unrelated errors are never retried.
func TestIndependentRootCleanup_shouldBoundDenialAndPreserveOtherErrors(t *testing.T) {
	root, info := ownedCleanupFixture(t)
	calls := 0
	started := time.Now()
	err := removeIndependentRootUsing(root, info, func(string) error { calls++; return windows.ERROR_SHARING_VIOLATION })
	if !errors.Is(err, windows.ERROR_SHARING_VIOLATION) || calls < 2 || time.Since(started) > 3*time.Second {
		t.Fatal("wrong retry bounds", calls, err)
	}
	other := errors.New("unclassified failure")
	calls = 0
	err = removeIndependentRootUsing(root, info, func(string) error { calls++; return other })
	if !errors.Is(err, other) || calls != 1 {
		t.Fatal("unrelated failure retried", calls, err)
	}
}
