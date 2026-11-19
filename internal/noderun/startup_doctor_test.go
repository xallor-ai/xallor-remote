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

// A target doctor performs authenticated remote Info. Only local diagnostics
// are allowed before selection; prove the remote path makes zero connections.
func TestStartupGate_should_blockTargetDoctor_beforeRelayConnection(t *testing.T) {
	var connections atomic.Int32
	relay := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		connections.Add(1)
		http.Error(w, "fixture refusal", http.StatusForbidden)
	}))
	defer relay.Close()
	profile := t.TempDir()
	t.Setenv("XALLOR_REMOTE_DATA_DIR", profile)
	store, err := identity.LoadFrom(profile)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetConfig("ws"+strings.TrimPrefix(relay.URL, "http"), t.TempDir(), "auto"); err != nil {
		t.Fatal(err)
	}
	if err := store.AddPeer("123456789", "fixture-grant"); err != nil {
		t.Fatal(err)
	}
	var selected atomic.Bool
	d := New(store, slog.New(slog.NewTextHandler(io.Discard, nil))).WithStartupGate(func() (bool, error) { return selected.Load(), nil })
	defer d.Stop()
	raw, server := net.Pipe()
	_ = raw.SetDeadline(time.Now().Add(5 * time.Second))
	done := make(chan struct{})
	go func() { defer close(done); d.serveIPC(ipc.NewConn(server)) }()
	defer func() { raw.Close(); <-done }()
	c := ipc.NewConn(raw)
	call := func(id, device string) ipc.Frame {
		t.Helper()
		if err := ipc.Call(c, id, "doctor", map[string]string{"device_id": device}); err != nil {
			t.Fatal(err)
		}
		result, err := c.ReadFrame()
		if err != nil {
			t.Fatal(err)
		}
		return result
	}
	local := call("local", "")
	if local.OK == nil || !*local.OK || connections.Load() != 0 {
		t.Fatal("local doctor used network", local)
	}
	remote := call("pending", "123456789")
	if remote.OK == nil || *remote.OK || remote.Code != protocol.RuntimePendingRecovery || connections.Load() != 0 {
		t.Fatalf("pending remote doctor bypassed gate: code=%s connections=%d", remote.Code, connections.Load())
	}
	selected.Store(true)
	remote = call("selected", "123456789")
	if remote.OK == nil || !*remote.OK || connections.Load() != 1 {
		t.Fatal("selected doctor did not probe", remote, connections.Load())
	}
}
