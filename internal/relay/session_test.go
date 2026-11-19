package relay

import (
	"context"
	"testing"
	"time"

	"github.com/xallor-ai/xallor-remote/internal/accesscode"
	"github.com/xallor-ai/xallor-remote/internal/admission"
	"github.com/xallor-ai/xallor-remote/internal/protocol"
)

func approvedSessionFixture(t *testing.T) (*relaySQLFixture, *Hub, *recSender, *recSender) {
	t.Helper()
	store := openIdentityTestStore(t)
	h := NewHub(store, nil, nil, nil, admission.New(admission.Limits{PerDevice: protocol.MaxDeviceRequests}), store.SQLStore)
	device, client := &recSender{}, &recSender{}
	if code := h.RegisterDevice("device", "secret", true, device); code != "" {
		t.Fatal(code)
	}
	grant := "xr_grant_transport"
	h.Handle(device, protocol.RoleDevice, "device", protocol.Message{Type: protocol.TypeDeviceSettings, RequestID: "settings", GrantHash: protocol.SHA256Hex(grant), Inbound: protocol.Bool(true)})
	if code := h.RegisterClient("device", grant, "", client); code != "" {
		t.Fatal(code)
	}
	target, err := store.LoadDevice(context.Background(), "device")
	if err != nil {
		t.Fatal(err)
	}
	targetUID := target.UID
	now := time.Now().UTC()
	code := accesscode.Code{ID: "code-disconnect", DeviceUID: targetUID, Type: accesscode.OneTime, Generation: 1, IssuedAt: now, Scope: []string{protocol.OpInfo}, RequireApproval: true, State: accesscode.StateActive}
	if err := store.WriteAccessCode(context.Background(), func(tx accesscode.Transaction) error {
		return tx.InsertCode(context.Background(), code, protocol.SHA256Hex("xr_grant_code"))
	}); err != nil {
		t.Fatal(err)
	}
	expires := now.Add(time.Hour).Unix()
	h.Handle(client, protocol.RoleClient, "device", protocol.Message{Type: protocol.TypeSessionRequest, RequestID: "request-disconnect", SessionID: "session-disconnect", TargetID: "device", TargetUID: targetUID, CodeID: code.ID, CodeGeneration: 1, SessionScope: []string{protocol.OpInfo}, SessionExpiresAt: &expires, Grant: "xr_grant_code"})
	approval := device.last()
	allow := true
	h.Handle(device, protocol.RoleDevice, "device", protocol.Message{Type: protocol.TypeSessionApprovalResult, RequestID: approval.RequestID, SessionID: approval.SessionID, RequestSHA256: approval.RequestSHA256, Allow: &allow})
	if active := client.last(); active.Status != protocol.SessionActive {
		t.Fatalf("session did not activate: %+v", active)
	}
	return store, h, device, client
}

// An issued resume capability keeps the durable session active across a client
// transport disconnect; the capability is the only proof that permits rebinding.
func TestSession_shouldRetainActiveAuthorization_whenResumeCapabilityExists(t *testing.T) {
	store, h, _, client := approvedSessionFixture(t)
	h.Drop(client, protocol.RoleClient, "device")
	if err := store.ReadAccessCode(context.Background(), func(reader accesscode.Reader) error {
		session, err := reader.Session(context.Background(), "session-disconnect")
		if err != nil || session.State != accesscode.SessionActive {
			t.Fatalf("client disconnect lost resumable session: %+v err=%v", session, err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

// A separately authenticated client may consume one resume capability once;
// the old capability is rejected and the rotated one becomes the new proof.
func TestSessionResume_shouldConsumeAndRotateCapability_whenBindingMatches(t *testing.T) {
	_, h, device, client := approvedSessionFixture(t)
	active := client.last()
	if active.ResumeID == "" || active.ResumeCapability == "" {
		t.Fatalf("fixture did not issue resume capability: %+v", active)
	}
	h.mu.Lock()
	targetUID := h.devices["device"].record.UID
	h.mu.Unlock()
	resumed := &recSender{}
	if code := h.RegisterClient("device", "xr_grant_transport", "", resumed); code != "" {
		t.Fatal(code)
	}
	h.Handle(resumed, protocol.RoleClient, "device", protocol.Message{Type: protocol.TypeSessionResumeRequest, RequestID: "resume-request", SessionID: active.SessionID, ResumeID: active.ResumeID, ResumeCapability: active.ResumeCapability, TargetUID: targetUID})
	result := resumed.last()
	if result.Type != protocol.TypeSessionResumeResult || result.Status != protocol.SessionActive || result.ResumeID == "" || result.ResumeCapability == "" || result.ResumeID == active.ResumeID {
		t.Fatalf("resume result=%+v", result)
	}
	h.Handle(resumed, protocol.RoleClient, "device", protocol.Message{Type: protocol.TypeInvoke, ExecID: "resumed-info", SessionID: active.SessionID, Op: protocol.OpInfo})
	if routed := device.last(); routed.Type != protocol.TypeInvoke || routed.ExecID == "" {
		t.Fatalf("resumed session not routed=%+v", routed)
	}
	second := &recSender{}
	if code := h.RegisterClient("device", "xr_grant_transport", "", second); code != "" {
		t.Fatal(code)
	}
	h.Handle(second, protocol.RoleClient, "device", protocol.Message{Type: protocol.TypeSessionResumeRequest, RequestID: "resume-reuse", SessionID: active.SessionID, ResumeID: active.ResumeID, ResumeCapability: active.ResumeCapability, TargetUID: targetUID})
	if rejected := second.last(); rejected.Status != protocol.SessionRejectedState {
		t.Fatalf("consumed resume capability reused: %+v", rejected)
	}
}

// A client session remains pending until the target Runtime explicitly
// approves the digest-bound scope; only then may the session route invoke.
func TestSession_shouldRequireTargetApproval_beforeScopedInvoke(t *testing.T) {
	store := openIdentityTestStore(t)
	h := NewHub(store, nil, nil, nil, admission.New(admission.Limits{PerDevice: protocol.MaxDeviceRequests}), store.SQLStore)
	device, client := &recSender{}, &recSender{}
	if code := h.RegisterDevice("device", "secret", true, device); code != "" {
		t.Fatal(code)
	}
	grant := "xr_grant_transport"
	h.Handle(device, protocol.RoleDevice, "device", protocol.Message{Type: protocol.TypeDeviceSettings, RequestID: "settings", GrantHash: protocol.SHA256Hex(grant), Inbound: protocol.Bool(true)})
	if code := h.RegisterClient("device", grant, "", client); code != "" {
		t.Fatal(code)
	}
	targetUID := h.devices["device"].record.UID
	now := time.Now().UTC()
	code := accesscode.Code{ID: "code-1", DeviceUID: targetUID, Type: accesscode.OneTime, Generation: 1, IssuedAt: now, Scope: []string{protocol.OpInfo}, RequireApproval: true, State: accesscode.StateActive}
	if err := store.WriteAccessCode(context.Background(), func(tx accesscode.Transaction) error {
		return tx.InsertCode(context.Background(), code, protocol.SHA256Hex("xr_grant_code"))
	}); err != nil {
		t.Fatal(err)
	}
	expires := now.Add(time.Hour).Unix()
	h.Handle(client, protocol.RoleClient, "device", protocol.Message{Type: protocol.TypeSessionRequest, RequestID: "request-1", SessionID: "session-1", TargetID: "device", TargetUID: targetUID, CodeID: code.ID, CodeGeneration: 1, SessionScope: []string{protocol.OpInfo}, SessionExpiresAt: &expires, Grant: "xr_grant_code"})
	approval := device.last()
	if approval.Type != protocol.TypeSessionApprovalRequest || approval.SessionID != "session-1" || approval.RequestSHA256 == "" {
		t.Fatalf("approval request=%+v", approval)
	}
	if pending := client.last(); pending.Type != protocol.TypeSessionResult || pending.Status != protocol.SessionPending {
		t.Fatalf("pending result=%+v", pending)
	}
	h.Handle(client, protocol.RoleClient, "device", protocol.Message{Type: protocol.TypeInvoke, ExecID: "before-approval", SessionID: "session-1", Op: protocol.OpInfo})
	if denied := client.last(); denied.Type != protocol.TypeInvokeNack || denied.Code != protocol.SessionPending {
		t.Fatalf("pending session invoke=%+v", denied)
	}
	allow := true
	h.Handle(device, protocol.RoleDevice, "device", protocol.Message{Type: protocol.TypeSessionApprovalResult, RequestID: approval.RequestID, SessionID: approval.SessionID, RequestSHA256: approval.RequestSHA256, Allow: &allow})
	active := client.last()
	if active.Type != protocol.TypeSessionResult || active.Status != protocol.SessionActive {
		t.Fatalf("active result=%+v", active)
	}
	if active.CodeGeneration != 1 || len(active.SessionScope) != 1 || active.SessionScope[0] != protocol.OpInfo || active.SessionExpiresAt == nil || *active.SessionExpiresAt != expires {
		t.Fatalf("active result omitted session facts=%+v", active)
	}
	if active.ResumeID == "" || active.ResumeCapability == "" {
		t.Fatalf("active result omitted resume capability=%+v", active)
	}
	if deviceActive := device.last(); deviceActive.ResumeID != "" || deviceActive.ResumeCapability != "" {
		t.Fatalf("target Runtime received client resume capability=%+v", deviceActive)
	}
	if err := store.ReadAccessCode(context.Background(), func(reader accesscode.Reader) error {
		resume, err := reader.Resume(context.Background(), active.ResumeID)
		if err != nil || resume.State != accesscode.ResumeActive || resume.SessionID != "session-1" || resume.CapabilityHash != protocol.SHA256Hex(active.ResumeCapability) {
			t.Fatalf("resume=%+v err=%v", resume, err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	h.Handle(client, protocol.RoleClient, "device", protocol.Message{Type: protocol.TypeInvoke, ExecID: "session-exec", SessionID: "session-1", Op: protocol.OpInfo})
	if routed := device.last(); routed.Type != protocol.TypeInvoke || routed.Op != protocol.OpInfo {
		t.Fatalf("scoped invoke not routed=%+v", routed)
	}
	h.mu.Lock()
	revoked := h.invalidateOwnershipLocked("device")
	h.mu.Unlock()
	h.revokeSessions(revoked)
	if err := store.ReadAccessCode(context.Background(), func(reader accesscode.Reader) error {
		session, err := reader.Session(context.Background(), "session-1")
		if err != nil || session.State != accesscode.SessionRevoked {
			t.Fatalf("session was not persistently revoked: %+v err=%v", session, err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

// Losing the target while approval is pending preserves an unknown reservation
// and removes the transport client instead of silently resuming the session.
func TestSession_shouldMarkPendingUnknown_andDropClient_whenTargetDisconnects(t *testing.T) {
	store := openIdentityTestStore(t)
	h := NewHub(store, nil, nil, nil, admission.New(admission.Limits{PerDevice: protocol.MaxDeviceRequests}), store.SQLStore)
	device, client := &recSender{}, &recSender{}
	if code := h.RegisterDevice("device", "secret", true, device); code != "" {
		t.Fatal(code)
	}
	transportGrant := "xr_grant_transport"
	h.Handle(device, protocol.RoleDevice, "device", protocol.Message{Type: protocol.TypeDeviceSettings, RequestID: "settings", GrantHash: protocol.SHA256Hex(transportGrant), Inbound: protocol.Bool(true)})
	if code := h.RegisterClient("device", transportGrant, "", client); code != "" {
		t.Fatal(code)
	}
	targetUID := h.devices["device"].record.UID
	now := time.Now().UTC()
	code := accesscode.Code{ID: "code-1", DeviceUID: targetUID, Type: accesscode.OneTime, Generation: 1, IssuedAt: now, Scope: []string{protocol.OpInfo}, RequireApproval: true, State: accesscode.StateActive}
	if err := store.WriteAccessCode(context.Background(), func(tx accesscode.Transaction) error {
		return tx.InsertCode(context.Background(), code, protocol.SHA256Hex("xr_grant_code"))
	}); err != nil {
		t.Fatal(err)
	}
	expires := now.Add(time.Hour).Unix()
	h.Handle(client, protocol.RoleClient, "device", protocol.Message{Type: protocol.TypeSessionRequest, RequestID: "request-1", SessionID: "session-1", TargetID: "device", TargetUID: targetUID, CodeID: code.ID, CodeGeneration: 1, SessionScope: []string{protocol.OpInfo}, SessionExpiresAt: &expires, Grant: "xr_grant_code"})
	h.Drop(device, protocol.RoleDevice, "device")
	if err := store.ReadAccessCode(context.Background(), func(reader accesscode.Reader) error {
		reservation, err := reader.Reservation(context.Background(), "request-1")
		if err != nil || reservation.State != accesscode.ReservationUnknown {
			t.Fatalf("reservation=%+v err=%v", reservation, err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	h.Handle(client, protocol.RoleClient, "device", protocol.Message{Type: protocol.TypeInvoke, ExecID: "after-disconnect", SessionID: "session-1", Op: protocol.OpInfo})
	if got := client.last(); got.Type != protocol.TypeInvokeNack || got.Code != protocol.Unauthorized {
		t.Fatalf("disconnected session client still routed: %+v", got)
	}
}

// A first expired-operation observation persists session ended state instead
// of leaving an active row that only fails in memory.
func TestSession_shouldPersistEndedState_whenOperationObservesExpiry(t *testing.T) {
	store := openIdentityTestStore(t)
	h := NewHub(nil, nil, nil, nil, admission.New(admission.Limits{PerDevice: protocol.MaxDeviceRequests}), store.SQLStore)
	ctx := context.Background()
	now := time.Now().UTC()
	code := accesscode.Code{ID: "code-expiry", DeviceUID: "expiry-target", Type: accesscode.LongLived, Generation: 1, IssuedAt: now.Add(-time.Minute), Scope: []string{protocol.OpInfo}, State: accesscode.StateActive}
	if err := store.WriteAccessCode(ctx, func(tx accesscode.Transaction) error { return tx.InsertCode(ctx, code, protocol.SHA256Hex("secret")) }); err != nil {
		t.Fatal(err)
	}
	if err := store.WriteAccessCode(ctx, func(tx accesscode.Transaction) error {
		target := accesscode.TargetBinding{DeviceUID: code.DeviceUID, Epoch: "expiry-epoch"}
		if err := tx.ActivateSessionTarget(ctx, target, now); err != nil {
			return err
		}
		_, _, err := tx.ReserveCode(ctx, code.ID, protocol.SHA256Hex("secret"), "reservation-expiry", "session-expiry", now, now.Add(time.Minute), target)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	sessionExpiry := now.Add(100 * time.Millisecond)
	var session accesscode.SessionAuthorization
	if err := store.WriteAccessCode(ctx, func(tx accesscode.Transaction) error {
		_, _, created, err := tx.FinalizeReservation(ctx, "reservation-expiry", true, now, &sessionExpiry, code.Scope)
		session = created
		return err
	}); err != nil {
		t.Fatal(err)
	}
	client := &recSender{}
	h.mu.Lock()
	h.clients[client] = clientSlot{targetID: "device", sessionRequired: true, session: &session}
	h.mu.Unlock()
	time.Sleep(150 * time.Millisecond)
	if code := h.verifySessionAccess(client, "session-expiry", protocol.OpInfo); code != protocol.Unauthorized {
		t.Fatalf("expired session code=%s", code)
	}
	if err := store.ReadAccessCode(ctx, func(reader accesscode.Reader) error {
		got, err := reader.Session(ctx, "session-expiry")
		if err != nil || got.State != accesscode.SessionEnded {
			t.Fatalf("session state=%+v err=%v", got, err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
