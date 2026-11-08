package noderun

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/xallor-ai/xallor-remote/internal/identity"
	"github.com/xallor-ai/xallor-remote/internal/ipc"
	"github.com/xallor-ai/xallor-remote/internal/process"
	"github.com/xallor-ai/xallor-remote/internal/protocol"
)

// An unsupported request is refused before approval, lease negotiation,
// executable lookup, job reservation or OS start; only a terminal nack leaves.
func TestExecBackend_shouldRejectBeforeLaunchingOrReservingResources(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	frames := make(chan protocol.Message, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer c.CloseNow()
		var m protocol.Message
		if wsjson.Read(ctx, c, &m) == nil {
			frames <- m
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
	if _, err := s.SetInbound(true); err != nil {
		t.Fatal(err)
	}
	d := New(s, nil)
	defer d.Stop()
	defer d.handoffs.Close()
	defer d.jobs.Close()
	link := &wsConn{ctx: ctx, conn: c}
	d.runLocalExec(ctx, link, protocol.Message{ExecID: "backend-refused", Payload: json.RawMessage(`{"backend":"container","program":{"executable":"must-not-resolve"}}`)})
	select {
	case m := <-frames:
		if m.Type != protocol.TypeInvokeNack || m.Code != protocol.BackendUnavailable || m.ExecID != "backend-refused" {
			t.Fatal(m)
		}
	case <-ctx.Done():
		t.Fatal("missing pre-start refusal")
	}
	if _, err := os.Stat(filepath.Join(s.Dir, "jobs.json")); !os.IsNotExist(err) {
		t.Fatal("backend refusal reserved capacity", err)
	}
}

// The actual local IPC configuration endpoint must preserve the whole patch
// and provide a structured refusal for unsupported selection.
func TestConfigBackend_shouldReturnStructuredFailureAndPreserveConfig(t *testing.T) {
	s, err := identity.LoadFrom(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	d := New(s, nil)
	defer d.Stop()
	a, b := net.Pipe()
	defer b.Close()
	_ = b.SetDeadline(time.Now().Add(3 * time.Second))
	go d.serveIPC(ipc.NewConn(a))
	client := ipc.NewConn(b)
	before := s.ConfigSnapshot()
	for _, backend := range []string{"low_privilege", "cgroup", "container", "unknown"} {
		if err := ipc.Call(client, backend, "config.set", map[string]any{"execution_backend": backend, "relay_url": "ws://changed.invalid"}); err != nil {
			t.Fatal(err)
		}
		frame, err := client.ReadFrame()
		if err != nil || frame.OK == nil || *frame.OK || frame.Code != protocol.BackendUnavailable || frame.Message == "" {
			t.Fatal("unstructured backend refusal", frame, err)
		}
		if s.ConfigSnapshot() != before {
			t.Fatal("unsupported backend changed config")
		}
	}
	if err := ipc.Call(client, "native", "config.set", map[string]string{"execution_backend": process.NativeUser}); err != nil {
		t.Fatal(err)
	}
	frame, err := client.ReadFrame()
	if err != nil || frame.OK == nil || !*frame.OK {
		t.Fatal(frame, err)
	}
}

// A backend error only proves no start when neither process creation nor
// cleanup/release uncertainty is present. Never convert unknown into a nack.
func TestBackendFailure_shouldPreserveCleanupUncertainty(t *testing.T) {
	ctx := context.Background()
	if m := failedStartMessage(ctx, "id", false, process.ErrBackendUnavailable, nil); m.Type != protocol.TypeInvokeNack || m.Code != protocol.BackendUnavailable {
		t.Fatal(m)
	}
	for _, tc := range []struct {
		started        bool
		start, release error
	}{
		{true, process.ErrBackendUnavailable, nil},
		{false, errors.Join(process.ErrBackendUnavailable, process.ErrCleanup), nil},
		{false, process.ErrBackendUnavailable, errors.New("release uncertain")},
	} {
		m := failedStartMessage(ctx, "id", tc.started, tc.start, tc.release)
		if m.Type != protocol.TypeError || m.Code != protocol.AgentError {
			t.Fatal("backend error concealed uncertainty", m)
		}
	}
}
