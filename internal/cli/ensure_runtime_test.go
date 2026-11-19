package cli

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"syscall"
	"testing"
	"time"

	"github.com/xallor-ai/xallor-remote/internal/buildinfo"
	"github.com/xallor-ai/xallor-remote/internal/ipc"
)

// Only an explicitly absent/refused endpoint authorizes bootstrap. Permission
// failure or an expired observation says nothing about an existing owner's life.
func TestOwnerEnsure_should_notAuthorizeBootstrap_whenConnectionStateIsUnknown(t *testing.T) {
	for _, scenario := range []struct {
		name    string
		err     error
		handled bool
	}{
		{"missing", os.ErrNotExist, false},
		{"refused", &net.OpError{Op: "dial", Net: "unix", Err: syscall.ECONNREFUSED}, false},
		{"permission", os.ErrPermission, true},
		{"deadline", context.DeadlineExceeded, true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			handled, err := ownerEnsureWithDial(func(time.Duration) (net.Conn, error) { return nil, scenario.err })
			if handled != scenario.handled || (err != nil) != scenario.handled {
				t.Fatal("connection error changed bootstrap authority", handled, err)
			}
		})
	}
}

// A successful envelope alone is not owner readiness. Exercise the actual pipe
// client against terminal replies lacking the required, bound runtime identity.
func TestOwnerEnsure_should_rejectSuccessWithoutReadyIdentity(t *testing.T) {
	for _, result := range []string{`{}`, `{"runtime":{"state":"pending"}}`, `{"runtime":{"state":"ready"}}`} {
		t.Run(result, func(t *testing.T) {
			t.Setenv("XALLOR_REMOTE_DATA_DIR", t.TempDir())
			ln, err := ipc.Listen()
			if err != nil {
				t.Fatal(err)
			}
			defer ln.Close()
			done := make(chan error, 1)
			go func() {
				conn, err := ln.Accept()
				if err != nil {
					done <- err
					return
				}
				defer conn.Close()
				_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
				server := ipc.NewConn(conn)
				req, err := server.ReadRequest()
				if err != nil {
					done <- err
					return
				}
				done <- server.WriteFrame(ipc.OK(req.ID, json.RawMessage(result)))
			}()
			handled, err := ownerEnsure()
			if !handled || err == nil {
				t.Fatal("incomplete ready reply accepted", handled, err)
			}
			if err := <-done; err != nil {
				t.Fatal(err)
			}
		})
	}
}

// A valid response is tied to this caller's protocol/profile, request ID and a
// complete identity. The helper below is only the controlled IPC peer.
func readyOwnerReply(conn net.Conn) error {
	defer conn.Close()
	c := ipc.NewConn(conn)
	req, err := c.ReadRequest()
	if err != nil {
		return err
	}
	return c.WriteFrame(ipc.OK(req.ID, map[string]any{"runtime": map[string]any{
		"state": "ready", "version": buildinfo.Version(), "revision": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		"protocol": buildinfo.Protocol, "profile_format": buildinfo.ProfileFormat(), "pid": 123,
		"sha256": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "activation_id": "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", "launch_id": "cccccccccccccccccccccccccccccccc"}}))
}

// A complete same-protocol ready result remains usable by the shared CLI head.
func TestOwnerEnsure_should_acceptCompleteReadyIdentity(t *testing.T) {
	t.Setenv("XALLOR_REMOTE_DATA_DIR", t.TempDir())
	ln, err := ipc.Listen()
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	done := make(chan error, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			done <- err
			return
		}
		_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
		done <- readyOwnerReply(conn)
	}()
	if handled, err := ownerEnsure(); !handled || err != nil {
		t.Fatal(handled, err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}
