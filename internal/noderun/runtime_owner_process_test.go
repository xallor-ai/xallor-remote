package noderun

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/xallor-ai/xallor-remote/internal/ipc"
	"github.com/xallor-ai/xallor-remote/internal/release"
	"github.com/xallor-ai/xallor-remote/internal/runtimeprocess"
)

// A real external helper uses the same CommandContext+Wait cancellation shape
// as source verification. Production Run must wait for it before returning.
func TestOwnerObservation_should_reapExternalVerifier_beforeDaemonRunReturns(t *testing.T) {
	t.Setenv("XALLOR_REMOTE_RELAY_URL", "ws://127.0.0.1:1/remote")
	store := ownerTestStore(t)
	d := New(store, slog.New(slog.NewTextHandler(io.Discard, nil)))
	runDone := make(chan error, 1)
	go func() { runDone <- d.Run() }()
	defer d.Stop()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	for {
		conn, err := ipc.Dial(50 * time.Millisecond)
		if err == nil {
			_ = conn.Close()
			break
		}
		if ctx.Err() != nil {
			t.Fatal("Runtime IPC not available", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()
	ready := make(chan *exec.Cmd, 1)
	failed := make(chan error, 1)
	d.startOwnerObservation(ipc.NewConn(a), ipc.Request{ID: "child", Method: "runtime.status"}, func(ctx context.Context) (release.RuntimeObservation, error) {
		cmd := exec.CommandContext(ctx, self, "-test.run=^TestOwnerVerifierChild$")
		quietOwnerVerifier(cmd)
		cmd.Env = append(os.Environ(), "XR_OWNER_VERIFIER_CHILD=1")
		output, err := cmd.StdoutPipe()
		if err != nil {
			failed <- err
			return release.RuntimeObservation{}, err
		}
		if err := cmd.Start(); err != nil {
			failed <- err
			return release.RuntimeObservation{}, err
		}
		line, err := bufio.NewReader(output).ReadString('\n')
		if err != nil || line != "ready\n" {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
			err = fmt.Errorf("verifier fixture readiness: %q %v", line, err)
			failed <- err
			return release.RuntimeObservation{}, err
		}
		ready <- cmd
		err = cmd.Wait()
		return release.RuntimeObservation{State: "unknown"}, err
	})
	var child *exec.Cmd
	select {
	case child = <-ready:
	case err := <-failed:
		t.Fatal(err)
	case <-ctx.Done():
		t.Fatal("verifier did not start")
	}
	watch, err := runtimeprocess.WatchExit(child.Process.Pid)
	if err != nil {
		t.Fatal(err)
	}
	defer watch.Close()
	d.Stop()
	select {
	case err := <-runDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("daemon failed to drain verifier")
	}
	if child.ProcessState == nil {
		t.Fatal("Run returned before verifier Wait")
	}
	if exited, err := watch.Exited(); err != nil || !exited {
		t.Fatal("verifier survived Runtime shutdown", exited, err)
	}
}

// This helper is entered only in the child process and has a finite lifetime.
func TestOwnerVerifierChild(t *testing.T) {
	if os.Getenv("XR_OWNER_VERIFIER_CHILD") != "1" {
		return
	}
	fmt.Println("ready")
	time.Sleep(30 * time.Second)
	os.Exit(0)
}
