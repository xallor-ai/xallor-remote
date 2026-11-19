package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/xallor-ai/xallor-remote/internal/buildinfo"
	"github.com/xallor-ai/xallor-remote/internal/ipc"
)

// A valid session response can follow human approval after the ordinary RPC
// deadline. The real OS IPC connection must survive and deliver it once.
func TestSessionRPC_shouldReceiveDelayedResult_onSameAdmittedConnection(t *testing.T) {
	for _, method := range []string{"session.request", "session.resume"} {
		t.Run(method, func(t *testing.T) {
			done := delayedRPCPeer(t, method)
			result, err := rpc(method, map[string]any{"device_id": "123456789"})
			peerErr := <-done
			if err != nil {
				t.Fatalf("session response was abandoned before the approved result: %v", err)
			}
			if peerErr != nil {
				t.Fatal(peerErr)
			}
			if result["session_id"] != "approved-session" || result["resume_capability"] != "controlled-proof" {
				t.Fatal("approved identity or resume proof was not delivered")
			}
		})
	}
}

// Extending consent observation must not extend unrelated calls: a silent
// ordinary status query still times out before this peer's eleven-second reply.
func TestRPC_shouldKeepOrdinaryDeadline_whenSessionWaitingIsExtended(t *testing.T) {
	done := delayedRPCPeer(t, "status")
	_, err := rpc("status", nil)
	<-done // The timed-out caller may close before the delayed write.
	var timeout interface{ Timeout() bool }
	if !errors.As(err, &timeout) || !timeout.Timeout() {
		t.Fatal("ordinary RPC did not retain its timeout", err)
	}
}

// Serve one bounded peer over the actual pipe/socket. This substitutes only the
// remote approval wait, not the production RPC handshake, transport or deadline.
func delayedRPCPeer(t *testing.T, method string) <-chan error {
	t.Helper()
	root, err := os.MkdirTemp("", "xr-cli-rpc-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(root); err != nil {
			t.Error(err)
		}
	})
	t.Setenv("XALLOR_REMOTE_DATA_DIR", root)
	listener, err := ipc.Listen()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	done := make(chan error, 1)
	go func() {
		done <- func() error {
			raw, err := listener.Accept()
			if err != nil {
				return err
			}
			defer raw.Close()
			// No successor connection is accepted; a retry cannot hide a lost result.
			_ = listener.Close()
			if err := raw.SetDeadline(time.Now().Add(20 * time.Second)); err != nil {
				return err
			}
			conn := ipc.NewConn(raw)
			request, err := conn.ReadRequest()
			if err != nil {
				return err
			}
			if method != "status" {
				if request.Method != "runtime.ensure" {
					return fmt.Errorf("business request preceded owner admission: %s", request.Method)
				}
				ready := map[string]any{"runtime": map[string]any{
					"state": "ready", "version": buildinfo.Version(), "revision": strings.Repeat("a", 40),
					"protocol": buildinfo.Protocol, "profile_format": buildinfo.ProfileFormat(), "pid": 123,
					"sha256": strings.Repeat("b", 64), "activation_id": strings.Repeat("c", 32), "launch_id": strings.Repeat("d", 32),
				}}
				if err := conn.WriteFrame(ipc.OK(request.ID, ready)); err != nil {
					return err
				}
				request, err = conn.ReadRequest()
				if err != nil {
					return err
				}
			}
			if request.Method != method {
				return fmt.Errorf("unexpected business request: %s", request.Method)
			}
			time.Sleep(11 * time.Second)
			if err := conn.WriteFrame(ipc.OK(request.ID, map[string]any{
				"session_id": "approved-session", "resume_capability": "controlled-proof",
			})); err != nil {
				return err
			}
			_, err = conn.ReadRequest()
			if !errors.Is(err, io.EOF) {
				return fmt.Errorf("completed connection was not closed without replay: %w", err)
			}
			return nil
		}()
	}()
	return done
}
