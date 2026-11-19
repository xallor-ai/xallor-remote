package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/xallor-ai/xallor-remote/internal/process"
	"github.com/xallor-ai/xallor-remote/internal/runtimeprocess"
)

// A successful installer parent must not leave an ordinary inherited child running.
func TestSuperviseShouldSettleDescendantAfterSuccessfulRoot(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XR_PACKAGE_SUPERVISOR_ROLE", "parent")
	t.Setenv("XR_PACKAGE_SUPERVISOR_DIR", dir)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	type completion struct {
		result observation
		err    error
	}
	done := make(chan completion, 1)
	go func() {
		result, err := supervise(ctx, []string{os.Args[0], "-test.run=^TestPackageSupervisorFixture$"})
		done <- completion{result, err}
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			t.Error("supervisor did not settle")
		}
	})
	var pid int
	for {
		if data, err := os.ReadFile(filepath.Join(dir, "leaf.pid")); err == nil {
			pid, err = strconv.Atoi(string(data))
			if err != nil {
				t.Fatal(err)
			}
			break
		}
		if ctx.Err() != nil {
			t.Fatal(ctx.Err())
		}
		time.Sleep(10 * time.Millisecond)
	}
	watch, err := runtimeprocess.WatchExit(pid)
	if err != nil {
		t.Fatal(err)
	}
	defer watch.Close()
	if exited, err := watch.Exited(); err != nil || exited {
		t.Fatalf("leaf was not observed live: %v %v", exited, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "release"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	result := <-done
	done <- result
	if result.err != nil || !result.result.CleanupConfirmed || !result.result.Started || result.result.ExitCode != 0 {
		t.Fatalf("result=%+v err=%v", result.result, result.err)
	}
	if exited, err := watch.Exited(); err != nil || !exited {
		t.Fatalf("leaf survived successful supervision: %v %v", exited, err)
	}
}

// A missing input must not be reported as a successful settled installation.
func TestSuperviseShouldRejectMissingProgram(t *testing.T) {
	result, err := supervise(context.Background(), []string{filepath.Join(t.TempDir(), "missing")})
	if err == nil || result.Started {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

// Subprocess-only fixture; no external shell or installation is used.
func TestPackageSupervisorFixture(t *testing.T) {
	dir := os.Getenv("XR_PACKAGE_SUPERVISOR_DIR")
	switch os.Getenv("XR_PACKAGE_SUPERVISOR_ROLE") {
	case "leaf":
		if os.WriteFile(filepath.Join(dir, "leaf.pid"), []byte(strconv.Itoa(os.Getpid())), 0600) != nil {
			os.Exit(3)
		}
		time.Sleep(60 * time.Second)
		os.Exit(0)
	case "parent":
		child := exec.Command(os.Args[0], "-test.run=^TestPackageSupervisorFixture$")
		child.Env = append(os.Environ(), "XR_PACKAGE_SUPERVISOR_ROLE=leaf")
		process.HideConsole(child)
		if child.Start() != nil {
			os.Exit(2)
		}
		for {
			if _, err := os.Stat(filepath.Join(dir, "release")); err == nil {
				os.Exit(0)
			}
			time.Sleep(10 * time.Millisecond)
		}
	default:
		t.Skip("subprocess-only fixture")
	}
}
