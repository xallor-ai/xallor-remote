//go:build windows

package release

import (
	"context"
	"errors"
	"github.com/xallor-ai/xallor-remote/internal/releasetrust"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

// A real Windows handle without delete sharing keeps the snapshot on disk;
// successful verification must not hide that failed cleanup.
func TestSourceCleanup_should_refuseSuccess_whenWindowsFileHeld(t *testing.T) {
	dir, _ := sourceBundle(t, "held-cleanup")
	var held syscall.Handle
	var snapshot string
	t.Cleanup(func() {
		if held != 0 {
			_ = syscall.CloseHandle(held)
		}
		entries, _ := os.ReadDir(snapshot)
		for _, e := range entries {
			if !e.IsDir() {
				_ = os.Remove(filepath.Join(snapshot, e.Name()))
			}
		}
		if snapshot != "" {
			_ = os.Remove(snapshot)
		}
	})
	got, err := withSourceSnapshot(context.Background(), dir, "", func(_ context.Context, file string, _ releasetrust.Identity, _, _ string) error {
		if held != 0 {
			return nil
		}
		snapshot = filepath.Dir(file)
		path, e := syscall.UTF16PtrFromString(file)
		if e != nil {
			return e
		}
		held, e = syscall.CreateFile(path, syscall.GENERIC_READ, syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE, nil, syscall.OPEN_EXISTING, 0, 0)
		return e
	}, nil)
	if !errors.Is(err, ErrCleanupUncertain) || got != (Manifest{}) {
		t.Fatalf("held snapshot returned success: %+v %v", got, err)
	}
	if _, e := os.Stat(snapshot); e != nil {
		t.Fatal("held evidence lost", e)
	}
}
