package runtimeprocess

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"testing"
	"time"
)

// A context timeout only stops observation, not the process. The same kernel
// handle must later confirm the real child's exit after its input is closed.
func TestWatchExit_should_waitForProcessExit_withoutTerminatingOnTimeout(t *testing.T) {
	if runtime.GOOS != "windows" && runtime.GOOS != "linux" {
		t.Skip("kernel exit observation is not implemented on this platform")
	}
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, self, "-test.run=^TestExitWatchChild$")
	configureTestChild(cmd)
	cmd.Env = append(os.Environ(), "XR_TEST_EXIT_WATCH_CHILD=1")
	input, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	output, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = input.Close()
		if cmd.ProcessState == nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
	}()
	ready, err := bufio.NewReader(output).ReadString('\n')
	if err != nil || ready != "ready\n" {
		t.Fatal("child did not become ready", ready, err)
	}
	watch, err := WatchExit(cmd.Process.Pid)
	if err != nil {
		t.Fatal(err)
	}
	defer watch.Close()
	if exited, err := watch.Exited(); err != nil || exited {
		t.Fatal("live child snapshot reported exit", exited, err)
	}
	short, done := context.WithTimeout(context.Background(), 20*time.Millisecond)
	err = watch.Wait(short)
	done()
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("live child reported stopped", err)
	}
	if err := input.Close(); err != nil {
		t.Fatal(err)
	}
	if err := watch.Wait(ctx); err != nil {
		t.Fatal("child exit not observed", err)
	}
	if exited, err := watch.Exited(); err != nil || !exited {
		t.Fatal("exited child snapshot missing", exited, err)
	}
	if err := cmd.Wait(); err != nil {
		t.Fatal(err)
	}
}

// The helper has no IPC listener: observation must depend on its OS lifetime.
func TestExitWatchChild(t *testing.T) {
	if os.Getenv("XR_TEST_EXIT_WATCH_CHILD") != "1" {
		return
	}
	fmt.Println("ready")
	_, _ = io.Copy(io.Discard, os.Stdin)
	os.Exit(0)
}
