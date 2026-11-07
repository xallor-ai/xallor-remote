package diagnostics

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/xallor-ai/xallor-remote/internal/protocol"
)

// A Relay which ignores an account token must not be diagnosed as authorized,
// even if it would subsequently allow a valid shared-code info operation.
func TestProbe_should_stopBeforeInvoke_whenAccountAuthenticationUnconfirmed(t *testing.T) {
	var invoked atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer c.CloseNow()
		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
		defer cancel()
		var msg protocol.Message
		if wsjson.Read(ctx, c, &msg) != nil {
			return
		}
		if wsjson.Write(ctx, c, protocol.Message{Type: protocol.TypeHelloOK, Role: protocol.RoleClient}) != nil {
			return
		}
		if wsjson.Read(ctx, c, &msg) != nil {
			return
		}
		invoked.Store(true)
		_ = wsjson.Write(ctx, c, protocol.Message{Type: protocol.TypeStdout, ExecID: msg.ExecID, Data: `{"device_id":"123456789"}`})
		_ = wsjson.Write(ctx, c, protocol.Message{Type: protocol.TypeExit, ExecID: msg.ExecID, Status: protocol.ExitCompleted, ExitCode: protocol.Int(0)})
	}))
	defer srv.Close()
	r := New("123456789")
	Probe(context.Background(), "ws"+strings.TrimPrefix(srv.URL, "http"), "123456789", "test-grant", "xr_account_"+strings.Repeat("a", 64), &r)
	if r.OK || invoked.Load() {
		t.Fatal("unconfirmed account authentication allowed a probe invocation")
	}
	for _, check := range r.Checks {
		if check.Layer == "authorization" && check.State == "ok" {
			t.Fatal("unconfirmed account authentication reported success")
		}
	}
}
