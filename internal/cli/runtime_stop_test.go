package cli

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/xallor-ai/xallor-remote/internal/buildinfo"
	"github.com/xallor-ai/xallor-remote/internal/ipc"
)

// This process acknowledges stop and closes IPC, but stays alive until the
// parent closes stdin. It exposes the distinction between IPC and OS lifetime.
func runStopProcessFixture() int {
	_ = buildinfo.Running()
	unlock, err := lockRuntime()
	if err != nil {
		return 41
	}
	defer unlock()
	ln, err := ipc.Listen()
	if err != nil {
		return 42
	}
	defer ln.Close()
	fmt.Println("ready")
	raw, err := ln.Accept()
	if err != nil {
		return 43
	}
	defer raw.Close()
	c := ipc.NewConn(raw)
	for {
		req, err := c.ReadRequest()
		if err != nil {
			return 44
		}
		switch req.Method {
		case "status":
			if err := c.WriteFrame(ipc.OK(req.ID, map[string]any{"build": buildinfo.Running()})); err != nil {
				return 45
			}
		case "stop":
			if err := c.WriteFrame(ipc.OK(req.ID, map[string]any{})); err != nil {
				return 46
			}
			_ = ln.Close()
			_ = raw.Close()
			fmt.Println("ipc-closed")
			_, _ = io.Copy(io.Discard, os.Stdin)
			return 0
		default:
			return 47
		}
	}
}

// Exercise the production daemon's stop acknowledgement and rapid teardown,
// not only the controlled fixture. The profile and Relay endpoint are isolated.
func TestStop_should_confirmActualDaemonExit_onIsolatedProfile(t *testing.T) {
	t.Setenv("XALLOR_REMOTE_DATA_DIR", t.TempDir())
	t.Setenv("XALLOR_REMOTE_RELAY_URL", "ws://127.0.0.1:1/remote")
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, self)
	configureDetached(cmd)
	cmd.Env = append(os.Environ(), "XR_TEST_ACTUAL_RUNTIME=1")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if cmd.ProcessState == nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
	}()
	deadline := time.Now().Add(8 * time.Second)
	for {
		live, err := (runtimeControl{}).Running()
		if err == nil && live != nil {
			if live.PID != cmd.Process.Pid {
				t.Fatal("isolated endpoint belongs to another process")
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("isolated daemon did not become available", err)
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err := (runtimeControl{}).Stop(); err != nil {
		t.Fatal("production daemon stop was not confirmed", err)
	}
	if err := cmd.Wait(); err != nil {
		t.Fatal(err)
	}
}

// Stop must remain pending after the real pipe/socket closes, and finish only
// when the held process handle signals. No default profile or Relay is used.
func TestStop_should_waitForRealProcess_afterIPCHasClosed(t *testing.T) {
	t.Setenv("XALLOR_REMOTE_DATA_DIR", t.TempDir())
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, self)
	configureDetached(cmd)
	cmd.Env = append(os.Environ(), "XR_TEST_STOP_PROCESS=1")
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
	reader := bufio.NewReader(output)
	line, err := reader.ReadString('\n')
	if err != nil || line != "ready\n" {
		t.Fatal("fixture failed to start", line, err)
	}
	stopped := make(chan error, 1)
	command := cmdStop()
	var printed bytes.Buffer
	command.SetOut(&printed)
	go func() { stopped <- command.RunE(command, nil) }()
	line, err = reader.ReadString('\n')
	if err != nil || line != "ipc-closed\n" {
		t.Fatal("fixture did not close IPC", line, err)
	}
	select {
	case err := <-stopped:
		t.Fatal("Stop returned before the real process exited", err)
	case <-time.After(150 * time.Millisecond):
	}
	if err := input.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-stopped:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("Stop failed to observe process exit")
	}
	if err := cmd.Wait(); err != nil {
		t.Fatal(err)
	}
	if printed.String() != "已停止。\n" {
		t.Fatal("stop confirmation missing", printed.String())
	}
}
