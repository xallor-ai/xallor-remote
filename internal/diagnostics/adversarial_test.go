package diagnostics

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/xallor-ai/xallor-remote/internal/protocol"
)

// A successful transport is insufficient: wrong role, identity, terminal status and oversized content fail closed.
func TestProbe_should_rejectMisleadingResponses(t *testing.T) {
	for _, scenario := range []string{"wrong_role", "wrong_device", "wrong_exit", "oversized"} {
		t.Run(scenario, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				c, err := websocket.Accept(w, req, nil)
				if err != nil {
					return
				}
				defer c.CloseNow()
				ctx, cancel := context.WithTimeout(req.Context(), time.Second)
				defer cancel()
				var msg protocol.Message
				if wsjson.Read(ctx, c, &msg) != nil {
					return
				}
				role := protocol.RoleClient
				if scenario == "wrong_role" {
					role = protocol.RoleDevice
				}
				_ = wsjson.Write(ctx, c, protocol.Message{Type: protocol.TypeHelloOK, Role: role})
				if wsjson.Read(ctx, c, &msg) != nil {
					return
				}
				if scenario == "wrong_role" {
					t.Error("must not invoke after wrong hello role")
					return
				}
				body := `{"device_id":"123456789"}`
				if scenario == "wrong_device" {
					body = `{"device_id":"987654321"}`
				}
				if scenario == "oversized" {
					body = strings.Repeat("x", 64*1024+1)
				}
				_ = wsjson.Write(ctx, c, protocol.Message{Type: protocol.TypeStdout, ExecID: msg.ExecID, Data: body})
				status := protocol.ExitCompleted
				if scenario == "wrong_exit" {
					status = protocol.ExitCancelled
				}
				zero := 0
				_ = wsjson.Write(ctx, c, protocol.Message{Type: protocol.TypeExit, ExecID: msg.ExecID, Status: status, ExitCode: &zero})
				_ = wsjson.Read(ctx, c, &msg)
			}))
			defer srv.Close()
			r := New("123456789")
			Probe(context.Background(), "ws"+strings.TrimPrefix(srv.URL, "http"), "123456789", "grant", "", &r)
			if r.OK {
				t.Fatalf("misleading response accepted: %s", scenario)
			}
		})
	}
}
