package handoff

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

// A normal probe exit reclaims descendants, releasing inherited stdout without
// waiting for the caller deadline or reporting a false pipe-drain failure.
func TestProcess_should_boundInheritedProbePipe(t *testing.T) {
	if role := os.Getenv("XR_HANDOFF_PIPE_HELPER"); role != "" {
		if role == "child" {
			time.Sleep(30 * time.Second)
			os.Exit(0)
		}
		child := exec.Command(os.Args[0], "-test.run=^TestProcess_should_boundInheritedProbePipe$")
		child.Env = append(os.Environ(), "XR_HANDOFF_PIPE_HELPER=child")
		child.Stdout = os.Stdout
		if err := child.Start(); err != nil {
			os.Exit(2)
		}
		_ = os.WriteFile(os.Getenv("XR_HANDOFF_PIPE_PID"), []byte(strconv.Itoa(child.Process.Pid)), 0600)
		os.Exit(0)
	}
	pidFile := filepath.Join(t.TempDir(), "child.pid")
	defer func() {
		b, err := os.ReadFile(pidFile)
		if err == nil {
			pid, _ := strconv.Atoi(string(b))
			if pid > 0 {
				if p, err := os.FindProcess(pid); err == nil {
					_ = p.Kill()
				}
			}
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	m := newTestManager(ctx, t.TempDir())
	defer m.Close()
	cmd := exec.Command(os.Args[0], "-test.run=^TestProcess_should_boundInheritedProbePipe$")
	cmd.Env = append(os.Environ(), "XR_HANDOFF_PIPE_HELPER=parent", "XR_HANDOFF_PIPE_PID="+pidFile)
	owner := m.probeOwner(t)
	defer owner.Finish()
	_, err := m.output(ctx, cmd, owner)
	if err != nil {
		t.Fatalf("supervised probe did not finish: %v", err)
	}
	if ctx.Err() != nil {
		t.Fatal("probe escaped its deadline")
	}
}
