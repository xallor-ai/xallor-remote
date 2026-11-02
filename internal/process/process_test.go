package process

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// A separate test process supplies deterministic output, exit and cancellation
// behavior without depending on shell quoting or installed scripting runtimes.
func TestProcessHelper(t *testing.T) {
	switch os.Getenv("XR_PROCESS_HELPER") {
	case "output":
		fmt.Print(strings.Repeat("中", 40000))
		fmt.Fprint(os.Stderr, "stderr preserved")
		os.Exit(7)
	case "wait":
		if os.WriteFile(os.Getenv("XR_PROCESS_READY"), nil, 0600) != nil {
			os.Exit(2)
		}
		time.Sleep(60 * time.Second)
		os.Exit(0)
	}
}

func helper(mode string) *exec.Cmd {
	cmd := exec.Command(os.Args[0], "-test.run=^TestProcessHelper$")
	cmd.Env = append(os.Environ(), "XR_PROCESS_HELPER="+mode)
	return cmd
}

// Cleanup must neither discard buffered Unicode output nor replace the root's
// real exit code. Repeated Wait/Stop after completion must not touch another PID.
func TestProcess_should_preserveOutputAndExit_afterNormalCleanup(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := helper("output")
	var out, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &stderr
	child, err := Start(ctx, cmd)
	if err != nil {
		t.Fatal(err)
	}
	err = child.Wait()
	var status *exec.ExitError
	if !errors.As(err, &status) || status.ExitCode() != 7 || errors.Is(err, ErrCleanup) {
		t.Fatal(err)
	}
	if out.String() != strings.Repeat("中", 40000) || stderr.String() != "stderr preserved" {
		t.Fatal("output lost")
	}
	for i := 0; i < 3; i++ {
		if child.Wait() != err || child.Stop() != nil {
			t.Fatal("completion not idempotent")
		}
	}
}

// Cancellation after confirmed startup stops the owned process. Concurrent
// callers may await the same cleanup without a second wait/reap or handle close.
func TestProcess_should_reapOnce_whenCancelledWithConcurrentWaiters(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := helper("wait")
	ready := filepath.Join(t.TempDir(), "ready")
	cmd.Env = append(cmd.Env, "XR_PROCESS_READY="+ready)
	child, err := Start(ctx, cmd)
	if err != nil {
		t.Fatal(err)
	}
	defer child.Stop()
	for {
		if _, err := os.Stat(ready); err == nil {
			break
		}
		if ctx.Err() != nil {
			t.Fatal(ctx.Err())
		}
		time.Sleep(10 * time.Millisecond)
	}
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := child.Wait()
			if err == nil || errors.Is(err, ErrCleanup) {
				t.Errorf("unexpected cancelled result: %v", err)
			}
		}()
	}
	cancel()
	wg.Wait()
}

// Already-cancelled contexts and missing executables must never leave a child.
func TestProcess_should_notStart_whenCancelledOrExecutableMissing(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	cmd := helper("wait")
	if _, err := Start(ctx, cmd); !errors.Is(err, context.Canceled) || cmd.Process != nil {
		t.Fatal("cancelled child started", err)
	}
	cmd = exec.Command(filepath.Join(t.TempDir(), "absent"))
	if _, err := Start(context.Background(), cmd); err == nil || cmd.Process != nil {
		t.Fatal("missing executable started")
	}
}
