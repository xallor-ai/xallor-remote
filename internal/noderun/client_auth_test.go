package noderun

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/xallor-ai/xallor-remote/internal/identity"
	"github.com/xallor-ai/xallor-remote/internal/protocol"
)

// A client link is publishable only after the Relay explicitly confirms the
// requested authentication mode; a generic greeting cannot imply account auth.
func TestClient_should_requireMatchingAuthenticationConfirmation(t *testing.T) {
	for _, tc := range []struct {
		name, mode, role, code string
		account, accepted      bool
	}{
		{name: "shared_confirmed", mode: "shared", role: "client", accepted: true},
		{name: "account_confirmed", mode: "account", role: "client", account: true, accepted: true},
		{name: "account_unconfirmed", role: "client", account: true},
		{name: "shared_unconfirmed", role: "client"},
		{name: "account_downgrade", mode: "shared", role: "client", account: true},
		{name: "unexpected_account", mode: "account", role: "client"},
		{name: "unknown_mode", mode: "other", role: "client", account: true},
		{name: "wrong_role", mode: "account", role: "device", account: true},
		{name: "ambiguous_error", mode: "account", role: "client", code: "unauthorized", account: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				c, err := websocket.Accept(w, r, nil)
				if err != nil {
					return
				}
				defer c.CloseNow()
				ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
				defer cancel()
				var hello protocol.Message
				if wsjson.Read(ctx, c, &hello) != nil {
					return
				}
				if (hello.AccountToken != "") != tc.account {
					t.Error("wrong authentication request")
				}
				ack := map[string]any{"type": "hello_ok", "role": tc.role}
				if tc.mode != "" {
					ack["client_auth"] = map[string]string{"mode": tc.mode}
				}
				if tc.code != "" {
					ack["code"] = tc.code
				}
				if wsjson.Write(ctx, c, ack) != nil {
					return
				}
				var next protocol.Message
				_ = wsjson.Read(ctx, c, &next)
			}))
			defer srv.Close()
			store, err := identity.LoadFrom(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			if err := store.SetConfig("ws"+strings.TrimPrefix(srv.URL, "http"), "", ""); err != nil {
				t.Fatal(err)
			}
			d := New(store, nil)
			defer d.Stop()
			if tc.account {
				if err := d.setAccount("xr_account_" + strings.Repeat("a", 64)); err != nil {
					t.Fatal(err)
				}
			}
			_, _, err = d.ensureClient(identity.Peer{DeviceID: "123456789", Grant: "test-grant"})
			if (err == nil) != tc.accepted {
				t.Errorf("connection accepted=%v, want %v; error=%v", err == nil, tc.accepted, err)
			}
			d.mu.Lock()
			published := len(d.clients)
			d.mu.Unlock()
			if !tc.accepted && published != 0 {
				t.Error("unconfirmed link entered the connection pool")
			}
		})
	}
}
