//go:build linux

package release

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/xallor-ai/xallor-remote/internal/releasetrust"
)

// Each child must reject an unwritten FIFO before reaching proof verification
// or consuming bytes. The external deadline bounds even a blocked open syscall.
func TestSourceSnapshot_should_rejectFIFO_withoutWaitingForWriter(t *testing.T) {
	if kind := os.Getenv("XR_SOURCE_FIFO_CHILD"); kind != "" {
		dir, m := bundle(t, "fifo")
		name := m.Artifact.Filename
		if kind == "manifest" {
			name = "manifest.json"
		}
		target := filepath.Join(dir, name)
		if err := os.Remove(target); err != nil {
			t.Fatal(err)
		}
		if err := syscall.Mkfifo(target, 0600); err != nil {
			t.Fatal(err)
		}
		called := false
		verify := func(context.Context, string, releasetrust.Identity, string, string) error { called = true; return nil }
		consume := func(string, Manifest, []byte) error { called = true; return nil }
		if _, err := withSourceSnapshot(context.Background(), dir, "", verify, consume); err == nil || called {
			t.Fatalf("nonregular candidate reached verification or consumption: %v", err)
		}
		return
	}
	for _, kind := range []string{"manifest", "binary"} {
		t.Run(kind, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestSourceSnapshot_should_rejectFIFO_withoutWaitingForWriter$", "-test.v")
			cmd.Env = append(os.Environ(), "XR_SOURCE_FIFO_CHILD="+kind)
			output, err := cmd.CombinedOutput()
			if ctx.Err() != nil {
				t.Fatalf("candidate open blocked: %s", kind)
			}
			if err != nil {
				t.Fatalf("child failed: %v\n%s", err, output)
			}
		})
	}
}

// A symlink outside the supplied root cannot introduce another file as a candidate.
func TestSourceSnapshot_should_refuseSymlinkEscape(t *testing.T) {
	dir, m := bundle(t, "escape")
	outside := filepath.Join(t.TempDir(), m.Artifact.Filename)
	data, err := os.ReadFile(filepath.Join(dir, m.Artifact.Filename))
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(outside, data, 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.Remove(filepath.Join(dir, m.Artifact.Filename)); err != nil {
		t.Fatal(err)
	}
	if err = os.Symlink(outside, filepath.Join(dir, m.Artifact.Filename)); err != nil {
		t.Fatal(err)
	}
	called := false
	verify := func(context.Context, string, releasetrust.Identity, string, string) error { called = true; return nil }
	if _, err = withSourceSnapshot(context.Background(), dir, "", verify, nil); err == nil || called {
		t.Fatal("root escape accepted", err)
	}
}
