package handoff

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

// A probe whose descendant retains stdout must return within the pipe-drain bound.
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
	m := New(ctx, t.TempDir(), Hooks{})
	defer m.Close()
	cmd := exec.Command(os.Args[0], "-test.run=^TestProcess_should_boundInheritedProbePipe$")
	cmd.Env = append(os.Environ(), "XR_HANDOFF_PIPE_HELPER=parent", "XR_HANDOFF_PIPE_PID="+pidFile)
	_, err := m.output(ctx, cmd)
	if !errors.Is(err, exec.ErrWaitDelay) {
		t.Fatalf("expected bounded pipe drain, got %v", err)
	}
	if ctx.Err() != nil {
		t.Fatal("probe escaped its deadline")
	}
}
