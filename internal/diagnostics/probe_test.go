package diagnostics

import (
	"context"
	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/xallor-ai/xallor-remote/internal/protocol"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// A disposable probe may only handshake and invoke info; errors must identify the actual failed layer.
func TestProbe_should_classify_and_never_executeCommands(t *testing.T) {
	for _, tc := range []struct{ code, layer, state string }{
		{protocol.Unauthorized, "authorization", "fail"}, {protocol.UnknownDevice, "target_online", "fail"},
		{protocol.DeviceOffline, "target_online", "fail"}, {protocol.InboundDisabled, "inbound", "fail"},
		{protocol.QuotaExceeded, "target_runtime", "unknown"}, {"", "target_runtime", "ok"},
	} {
		t.Run(tc.code, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				c, err := websocket.Accept(w, req, nil)
				if err != nil {
					return
				}
				defer c.CloseNow()
				ctx, cancel := context.WithTimeout(req.Context(), time.Second)
				defer cancel()
				var m protocol.Message
				if wsjson.Read(ctx, c, &m) != nil {
					return
				}
				if m.Type != protocol.TypeHelloClient || m.TargetID != "123456789" {
					t.Error("unexpected hello")
					return
				}
				if tc.code == protocol.Unauthorized || tc.code == protocol.UnknownDevice {
					_ = wsjson.Write(ctx, c, protocol.Message{Type: protocol.TypeError, Code: tc.code})
					return
				}
				_ = wsjson.Write(ctx, c, protocol.Message{Type: protocol.TypeHelloOK, Role: protocol.RoleClient})
				if wsjson.Read(ctx, c, &m) != nil {
					return
				}
				if m.Type != protocol.TypeInvoke || m.Op != protocol.OpInfo {
					t.Error("probe performed mutation")
					return
				}
				if tc.code != "" {
					_ = wsjson.Write(ctx, c, protocol.Nack(m.ExecID, tc.code))
					return
				}
				_ = wsjson.Write(ctx, c, protocol.Message{Type: protocol.TypeStdout, ExecID: m.ExecID, Data: `{"device_id":"123456789"}`})
				zero := 0
				_ = wsjson.Write(ctx, c, protocol.Message{Type: protocol.TypeExit, ExecID: m.ExecID, Status: protocol.ExitCompleted, ExitCode: &zero})
				// Keep the socket open until the client closes; CloseNow could discard buffered data.
				_ = wsjson.Read(ctx, c, &m)
			}))
			defer srv.Close()
			r := New("123456789")
			Probe(context.Background(), "ws"+strings.TrimPrefix(srv.URL, "http"), "123456789", "test-grant", &r)
			for _, check := range r.Checks {
				if check.Layer == tc.layer && (check.State != tc.state || check.Code != tc.code) {
					t.Fatalf("%+v", r)
				}
			}
			if r.OK != (tc.code == "") {
				t.Fatalf("%+v", r)
			}
		})
	}
}

// A server that never acknowledges must return unknown within the caller's deadline.
func TestProbe_should_boundSilentRelay_and_notClaimAuthorized(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		c, err := websocket.Accept(w, req, nil)
		if err != nil {
			return
		}
		defer c.CloseNow()
		var m protocol.Message
		for wsjson.Read(req.Context(), c, &m) == nil {
		}
	}))
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	r := New("123456789")
	Probe(ctx, "ws"+strings.TrimPrefix(srv.URL, "http"), "123456789", "test-grant", &r)
	if r.OK {
		t.Fatal("silent relay reported healthy")
	}
	for _, check := range r.Checks {
		if check.Layer == "authorization" && check.State == "ok" {
			t.Fatal("unconfirmed authorization")
		}
	}
}
