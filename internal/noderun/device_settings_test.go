package noderun

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/xallor-ai/xallor-remote/internal/identity"
	"github.com/xallor-ai/xallor-remote/internal/invocation"
	"github.com/xallor-ai/xallor-remote/internal/ipc"
	"github.com/xallor-ai/xallor-remote/internal/protocol"
)

type settingsWire struct {
	d                 *Daemon
	requests, replies chan protocol.Message
}

func newSettingsWire(t *testing.T) *settingsWire {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	f := &settingsWire{requests: make(chan protocol.Message, 8), replies: make(chan protocol.Message, 8)}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer c.CloseNow()
		for {
			var req protocol.Message
			if wsjson.Read(ctx, c, &req) != nil {
				return
			}
			select {
			case f.requests <- req:
			case <-ctx.Done():
				return
			}
			select {
			case reply := <-f.replies:
				if wsjson.Write(ctx, c, reply) != nil {
					return
				}
			case <-ctx.Done():
				return
			}
		}
	}))
	t.Cleanup(func() { cancel(); srv.Close() })
	st, err := identity.LoadFrom(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetConfig("ws"+srv.URL[4:], t.TempDir(), "auto"); err != nil {
		t.Fatal(err)
	}
	f.d = &Daemon{ctx: ctx, cancel: cancel, store: st, local: map[string]context.CancelFunc{}}
	return f
}

func (f *settingsWire) next(t *testing.T, kind string) protocol.Message {
	t.Helper()
	select {
	case m := <-f.requests:
		if m.Type != kind {
			t.Fatalf("expected %s, got %+v", kind, m)
		}
		return m
	case <-f.d.ctx.Done():
		t.Fatal("missing device message")
		return protocol.Message{}
	}
}
func (f *settingsWire) ack(m protocol.Message) {
	f.replies <- protocol.Message{Type: protocol.TypeDeviceSettingsResult, RequestID: m.RequestID, DeviceContext: &invocation.DeviceContext{Authority: strings.Repeat("a", 32), DeviceUID: strings.Repeat("b", 32), Inbound: *m.Inbound}}
}
func noSettingsResult(t *testing.T, done <-chan ipc.Frame) {
	t.Helper()
	select {
	case result := <-done:
		t.Fatalf("returned before Relay confirmation: %+v", result)
	case <-time.After(30 * time.Millisecond):
	}
}

// A local update racing initial registration must wait for the original
// snapshot's confirmation, then publish the newer complete snapshot. CLI success
// must wait for that second confirmation, including its generated grant.
func TestDeviceSettings_should_confirmLatestSnapshot_whenUpdateRacesConnection(t *testing.T) {
	f := newSettingsWire(t)
	connected := make(chan error, 1)
	go func() { connected <- f.d.connectDevice() }()
	f.next(t, protocol.TypeHelloDevice)
	done := make(chan ipc.Frame, 1)
	go func() { done <- f.d.updateDeviceSettings("on", protocol.Bool(true)) }()
	noSettingsResult(t, done)
	f.replies <- protocol.Message{Type: protocol.TypeHelloOK, Role: protocol.RoleDevice}
	initial := f.next(t, protocol.TypeDeviceSettings)
	if initial.GrantHash != "" || *initial.Inbound {
		t.Fatal("initial snapshot changed under handshake")
	}
	noSettingsResult(t, done)
	f.ack(initial)
	updated := f.next(t, protocol.TypeDeviceSettings)
	if !updated.ValidDeviceSettings() || !*updated.Inbound {
		t.Fatal("missing complete new settings")
	}
	noSettingsResult(t, done)
	f.ack(updated)
	result := <-done
	if result.OK == nil || !*result.OK {
		t.Fatalf("not confirmed: %+v", result)
	}
	var payload struct {
		Grant   string `json:"grant"`
		Inbound bool   `json:"inbound"`
	}
	if json.Unmarshal(result.Result, &payload) != nil || !payload.Inbound || protocol.SHA256Hex(payload.Grant) != updated.GrantHash {
		t.Fatal("confirmed state does not match returned grant")
	}
	f.d.Stop()
	<-connected
}

// Offline changes are persisted but must not claim Relay success. Reconnection
// synchronizes that exact grant, never reissues or replays the original operation.
func TestDeviceSettings_should_keepDesiredState_whenRelayIsUnavailable(t *testing.T) {
	f := newSettingsWire(t)
	result := f.d.updateDeviceSettings("offline", protocol.Bool(true))
	if result.OK == nil || *result.OK || result.Code != protocol.RelayError {
		t.Fatalf("false success: %+v", result)
	}
	_, _, grant, _, _, inbound, _ := f.d.store.Snapshot()
	if grant == "" || !inbound {
		t.Fatal("desired state not retained")
	}
	connected := make(chan error, 1)
	go func() { connected <- f.d.connectDevice() }()
	f.next(t, protocol.TypeHelloDevice)
	f.replies <- protocol.Message{Type: protocol.TypeHelloOK, Role: protocol.RoleDevice}
	m := f.next(t, protocol.TypeDeviceSettings)
	if m.GrantHash != protocol.SHA256Hex(grant) || !*m.Inbound {
		t.Fatal("reconnect changed desired state")
	}
	f.ack(m)
	f.d.Stop()
	<-connected
}

// Cancelling a queued writer must release its admission slot and leave the
// current writer's ownership intact; a seventeenth waiter is rejected.
func TestSettingsGate_should_boundAdmissionAndReleaseCancelledWaiter(t *testing.T) {
	var g settingsGate
	release, err := g.acquire(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for range 15 {
		g.slots <- struct{}{}
	}
	if _, err := g.acquire(context.Background()); err != errStr(protocol.QuotaExceeded) {
		t.Fatal("unbounded admission", err)
	}
	for range 15 {
		<-g.slots
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := g.acquire(ctx); err != context.Canceled {
		t.Fatal(err)
	}
	if len(g.slots) != 1 || len(g.writer) != 1 {
		t.Fatal("cancelled waiter changed ownership")
	}
	release()
	if len(g.slots) != 0 || len(g.writer) != 0 {
		t.Fatal("gate leaked")
	}
}

// A connected Relay can lose the acknowledgement after receiving a mutation.
// The bounded request must return unconfirmed, retain the saved desired state,
// release correlation, and disconnect instead of reporting success or replaying.
func TestDeviceSettings_should_reportUnconfirmed_whenAcknowledgementIsLost(t *testing.T) {
	f := newSettingsWire(t)
	connected := make(chan error, 1)
	go func() { connected <- f.d.connectDevice() }()
	f.next(t, protocol.TypeHelloDevice)
	f.replies <- protocol.Message{Type: protocol.TypeHelloOK, Role: protocol.RoleDevice}
	f.ack(f.next(t, protocol.TypeDeviceSettings))
	done := make(chan ipc.Frame, 1)
	go func() { done <- f.d.updateDeviceSettings("lost", protocol.Bool(true)) }()
	m := f.next(t, protocol.TypeDeviceSettings)
	result := <-done
	if result.OK == nil || *result.OK || result.Code != protocol.RelayError {
		t.Fatalf("unconfirmed mutation succeeded: %+v", result)
	}
	_, _, grant, _, _, inbound, _ := f.d.store.Snapshot()
	if !inbound || protocol.SHA256Hex(grant) != m.GrantHash {
		t.Fatal("desired state lost or reissued")
	}
	<-connected
	f.d.settingsReplies.mu.Lock()
	defer f.d.settingsReplies.mu.Unlock()
	if len(f.d.settingsReplies.pending) != 0 {
		t.Fatal("timed out correlation retained")
	}
}
