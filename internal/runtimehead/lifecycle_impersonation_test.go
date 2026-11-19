package runtimehead

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/xallor-ai/xallor-remote/internal/buildinfo"
	"github.com/xallor-ai/xallor-remote/internal/ipc"
	"github.com/xallor-ai/xallor-remote/internal/runtimeowner"
)

// A rogue endpoint can learn the queried ID and calculate a rollback intent.
// Correct response shape is not proof of an authenticated lifecycle observer.
func TestLifecycleQueryRejectsForgedSuccessfulEndpoint(t *testing.T) {
	t.Setenv("XALLOR_REMOTE_DATA_DIR", t.TempDir())
	listener, err := ipc.ListenLifecycle()
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	command, _ := runtimeowner.NewCommand("rollback", "")
	done := make(chan error, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			done <- err
			return
		}
		defer conn.Close()
		conn.SetDeadline(time.Now().Add(3 * time.Second))
		c := ipc.NewConn(conn)
		request, err := c.ReadRequest()
		if err != nil {
			done <- err
			return
		}
		done <- c.WriteFrame(ipc.OK(request.ID, map[string]any{"operation": successfulOperation(command), "settled": true, "observer": buildinfo.Running()}))
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if record, err := QueryLifecycle(ctx, command.ID); err == nil {
		t.Fatal("forged successful receipt accepted", record)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

// Even a correctly correlated success-shaped reply must be rejected at the
// namespace boundary before it can stand in for the requested profile's result.
func TestLifecycleQueryRejectsForeignNamespaceReceipt(t *testing.T) {
	t.Setenv("XALLOR_REMOTE_DATA_DIR", t.TempDir())
	listener, err := ipc.ListenLifecycle()
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	command, _ := runtimeowner.NewCommand("rollback", "")
	done := make(chan error, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			done <- err
			return
		}
		defer conn.Close()
		_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
		c := ipc.NewConn(conn)
		req, err := c.ReadRequest()
		if err != nil {
			done <- err
			return
		}
		r := successfulOperation(command)
		r.NamespaceSHA256 = strings.Repeat("f", 64)
		done <- c.WriteFrame(ipc.OK(req.ID, map[string]any{"operation": r, "settled": true, "observer": buildinfo.Running()}))
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := QueryLifecycle(ctx, command.ID); !errors.Is(err, runtimeowner.ErrInvalid) {
		t.Fatal("foreign receipt did not fail namespace validation", err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}
