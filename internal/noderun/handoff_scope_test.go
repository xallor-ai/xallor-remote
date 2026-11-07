package noderun

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/xallor-ai/xallor-remote/internal/handoff"
	"github.com/xallor-ai/xallor-remote/internal/identity"
	"github.com/xallor-ai/xallor-remote/internal/invocation"
	"github.com/xallor-ai/xallor-remote/internal/protocol"
)

// Even an established device link cannot omit or partially supply the trusted
// namespace. Reject before querying receipts or persisting a submission.
func TestHandoff_should_rejectMissingOrInvalidScope_beforeManagerAccess(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	results := make(chan protocol.Message, 4)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer c.CloseNow()
		for {
			var msg protocol.Message
			if wsjson.Read(ctx, c, &msg) != nil {
				return
			}
			results <- msg
		}
	}))
	defer srv.Close()
	c, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer c.CloseNow()
	s, err := identity.LoadFrom(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.SetInbound(true); err != nil {
		t.Fatal(err)
	}
	d := New(s, nil)
	defer d.Stop()
	defer d.handoffs.Close()
	link := &wsConn{ctx: ctx, conn: c}
	for _, scope := range []*invocation.Scope{nil, {Authority: strings.Repeat("a", 32)}} {
		for _, op := range []string{protocol.OpHandoffGet, protocol.OpHandoffSubmit} {
			d.runHandoff(link, protocol.Message{Type: protocol.TypeInvoke, ExecID: "denied", Op: op, Payload: json.RawMessage(`{}`), Scope: scope}, handoff.Ticket{})
			select {
			case got := <-results:
				if got.Type != protocol.TypeInvokeNack || got.Code != protocol.Unauthorized {
					t.Fatal(got)
				}
			case <-ctx.Done():
				t.Fatal("missing response")
			}
		}
	}
	if _, err := os.Stat(filepath.Join(s.Dir, "handoff", "requests")); !os.IsNotExist(err) {
		t.Fatal("invalid scope created records", err)
	}
}
