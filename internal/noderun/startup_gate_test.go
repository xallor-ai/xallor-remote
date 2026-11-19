package noderun

import (
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/xallor-ai/xallor-remote/internal/identity"
	"github.com/xallor-ai/xallor-remote/internal/ipc"
	"github.com/xallor-ai/xallor-remote/internal/protocol"
)

// Pending startup exposes diagnostics only. After the owner commits selection,
// the same IPC connection can perform a business operation without reconnecting.
func TestStartupGate_should_blockBusinessUntilSelected_butAllowDiagnostics(t *testing.T) {
	profile := t.TempDir()
	t.Setenv("XALLOR_REMOTE_DATA_DIR", profile)
	store, err := identity.LoadFrom(profile)
	if err != nil {
		t.Fatal(err)
	}
	var selected atomic.Bool
	d := New(store, slog.New(slog.NewTextHandler(io.Discard, nil))).WithStartupGate(func() (bool, error) { return selected.Load(), nil })
	defer d.Stop()
	client, server := net.Pipe()
	defer client.Close()
	_ = client.SetDeadline(time.Now().Add(3 * time.Second))
	done := make(chan struct{})
	go func() { defer close(done); d.serveIPC(ipc.NewConn(server)) }()
	defer func() { client.Close(); <-done }()
	c := ipc.NewConn(client)
	call := func(id, method string, params any) ipc.Frame {
		t.Helper()
		if err := ipc.Call(c, id, method, params); err != nil {
			t.Fatal(err)
		}
		frame, err := c.ReadFrame()
		if err != nil {
			t.Fatal(err)
		}
		return frame
	}
	params := map[string]string{"device_id": "fixture-peer", "grant": "fixture-grant"}
	frame := call("blocked", "peer.add", params)
	if frame.OK == nil || *frame.OK || frame.Code != protocol.RuntimePendingRecovery {
		t.Fatal("pending mutation accepted", frame)
	}
	if _, ok := store.Peer("fixture-peer"); ok {
		t.Fatal("pending mutation persisted")
	}
	frame = call("diagnostic", "status", nil)
	if frame.OK == nil || !*frame.OK {
		t.Fatal("pending diagnostic unavailable", frame)
	}
	selected.Store(true)
	frame = call("selected", "peer.add", params)
	if frame.OK == nil || !*frame.OK {
		t.Fatal("selected owner cannot serve", frame)
	}
	if _, ok := store.Peer("fixture-peer"); !ok {
		t.Fatal("selected mutation missing")
	}
}

// IPC may exist while the parent commits pending. Even an outbound connection
// to the Relay must wait for selection; no device credentials are sent early.
func TestStartupGate_should_delayRelayConnection_untilSelected(t *testing.T) {
	attempted := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		select {
		case attempted <- struct{}{}:
		default:
		}
		http.Error(w, "fixture refusal", http.StatusForbidden)
	}))
	defer server.Close()
	profile := t.TempDir()
	t.Setenv("XALLOR_REMOTE_DATA_DIR", profile)
	store, err := identity.LoadFrom(profile)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetConfig("ws"+strings.TrimPrefix(server.URL, "http"), t.TempDir(), "auto"); err != nil {
		t.Fatal(err)
	}
	var selected atomic.Bool
	var checks atomic.Int32
	waiting := make(chan struct{}, 1)
	d := New(store, slog.New(slog.NewTextHandler(io.Discard, nil))).WithStartupGate(func() (bool, error) {
		if checks.Add(1) == 3 {
			waiting <- struct{}{}
		}
		return selected.Load(), nil
	})
	done := make(chan struct{})
	go func() { defer close(done); d.connectSelectedDevice() }()
	defer func() { d.Stop(); <-done }()
	select {
	case <-waiting:
	case <-time.After(3 * time.Second):
		t.Fatal("startup did not wait for selection")
	}
	select {
	case <-attempted:
		t.Fatal("connected before selected")
	default:
	}
	selected.Store(true)
	select {
	case <-attempted:
	case <-time.After(3 * time.Second):
		t.Fatal("selected owner never connected")
	}
}
