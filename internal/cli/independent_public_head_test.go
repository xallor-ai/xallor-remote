package cli

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/xallor-ai/xallor-remote/internal/process"
)

// Launch the actual command dispatcher, not an in-process Cobra/RPC seam.
// The copied _test executable still uses a controlled source verifier; this
// is public-command-path acceptance, not a published/signed CLI assertion.
func (h *independentHarness) runPublicHead(p *independentProfile, args ...string) ([]byte, string, error) {
	h.t.Helper()
	var output, diagnostic bytes.Buffer
	var pid int
	err := h.inProfile(p, func() error {
		cmd := exec.Command(filepath.Join(h.bundle, h.manifest.Artifact.Filename), args...)
		cmd.Stdout, cmd.Stderr = &output, &diagnostic
		process.HideConsole(cmd)
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		child, err := process.Start(ctx, cmd)
		if err != nil {
			if errors.Is(err, process.ErrCleanup) {
				h.cleanupErrors = append(h.cleanupErrors, "public head startup cleanup unconfirmed")
				h.t.Fatal("public head startup cleanup unconfirmed", err)
			}
			return err
		}
		pid = cmd.Process.Pid
		err = child.Wait()
		if errors.Is(err, process.ErrCleanup) {
			h.cleanupErrors = append(h.cleanupErrors, "public head cleanup unconfirmed")
			h.t.Fatal("public head cleanup unconfirmed", err)
		}
		if cmd.ProcessState == nil || !cmd.ProcessState.Exited() {
			h.cleanupErrors = append(h.cleanupErrors, "public head exit not reaped")
			h.t.Fatal("public head exit not reaped")
		}
		if ctx.Err() != nil {
			h.t.Fatal("public head exceeded deadline; not an authorization refusal", ctx.Err())
		}
		return err
	})
	if pid > 0 {
		rows, _ := h.observations["public_cli_processes"].([]map[string]any)
		rows = append(rows, map[string]any{"pid": pid, "command": args[0], "reaped": true, "success": err == nil})
		h.observations["public_cli_processes"] = rows
	}
	return output.Bytes(), diagnostic.String(), err
}
