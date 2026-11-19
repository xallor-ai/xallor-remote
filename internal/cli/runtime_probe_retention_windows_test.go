//go:build windows

package cli

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/xallor-ai/xallor-remote/internal/process"
	"github.com/xallor-ai/xallor-remote/internal/runtimeimage"
	"github.com/xallor-ai/xallor-remote/internal/runtimeprocess"
)

// An opt-in owned-process diagnostic, not a release acceptance gate. Determine
// whether retaining an exited process observer retains its executable object.
func TestProbeImageRetentionDiagnostic(t *testing.T) {
	if os.Getenv("XR_PROBE_RETENTION_TRACE") != "1" {
		t.Skip("owned diagnostic is opt-in")
	}
	_, binary, manifest := nativeOwnerBundle(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, "version", "--json")
	process.HideConsole(cmd)
	var output bytes.Buffer
	cmd.Stdout = &output
	image, err := runtimeimage.Prepare(cmd, manifest.Artifact.SHA256)
	if err != nil {
		t.Fatal(err)
	}
	defer image.Close()
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if cmd.ProcessState == nil {
			cmd.Process.Kill()
			cmd.Wait()
		}
	}()
	watch, err := runtimeprocess.WatchExit(cmd.Process.Pid)
	if err != nil {
		t.Fatal(err)
	}
	defer watch.Close()
	if err := cmd.Wait(); err != nil {
		t.Fatal(err)
	}
	if err := image.Close(); err != nil {
		t.Fatal(err)
	}
	exited, err := watch.Exited()
	if err != nil || !exited {
		t.Fatal("probe did not exit", exited, err)
	}
	removeErr := os.Remove(binary)
	t.Logf("retained exited-process observer: delete=%v", removeErr)
	if err := watch.Close(); err != nil {
		t.Fatal(err)
	}
	if removeErr != nil {
		t.Logf("released process observer: delete=%v", os.Remove(binary))
	}
}
