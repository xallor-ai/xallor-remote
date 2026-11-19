package runtimehead

import (
	"encoding/json"
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/xallor-ai/xallor-remote/internal/ipc"
	"github.com/xallor-ai/xallor-remote/internal/protocol"
)

// Invalid or unrelated success envelopes never authorize business dispatch,
// and the caller must release the refused connection rather than reuse it.
func TestHandshake_should_closeConnection_whenReadinessIsInvalid(t *testing.T) {
	for _, scenario := range []struct {
		name  string
		frame ipc.Frame
	}{
		{"missing_identity", ipc.OK("runtime-owner", map[string]any{})},
		{"wrong_id", ipc.OK("another-request", map[string]any{})},
		{"event", ipc.Event("runtime-owner", "stdout", "anything")},
		{"missing_ok", ipc.Frame{ID: "runtime-owner"}},
		{"missing_code", ipc.Fail("runtime-owner", "", "failure")},
		{"pending_success", ipc.OK("runtime-owner", map[string]any{"runtime": map[string]any{"state": "pending"}})},
		{"malformed_result", ipc.Frame{ID: "runtime-owner", OK: boolPointer(true), Result: json.RawMessage(`[]`)}},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			client, server := net.Pipe()
			defer client.Close()
			defer server.Close()
			done := make(chan error, 1)
			go func() {
				_ = server.SetDeadline(time.Now().Add(3 * time.Second))
				c := ipc.NewConn(server)
				if _, err := c.ReadRequest(); err != nil {
					done <- err
					return
				}
				if err := c.WriteFrame(scenario.frame); err != nil {
					done <- err
					return
				}
				if _, err := c.ReadRequest(); err == nil {
					done <- fmt.Errorf("business request after invalid ready")
					return
				} else if e, ok := err.(net.Error); ok && e.Timeout() {
					done <- fmt.Errorf("connection not closed: %w", err)
					return
				}
				done <- nil
			}()
			conn, err := Handshake(client)
			if conn != nil || err == nil || ErrorCode(err) != protocol.RuntimeSourceUnverified {
				t.Errorf("invalid identity accepted: %v %v", conn, err)
			}
			if err := <-done; err != nil {
				t.Fatal(err)
			}
		})
	}
}

func boolPointer(v bool) *bool { return &v }
