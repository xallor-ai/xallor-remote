package cli

import (
	"context"
	"encoding/json"
	"github.com/xallor-ai/xallor-remote/internal/access"
	"github.com/xallor-ai/xallor-remote/internal/accesscode"
	"github.com/xallor-ai/xallor-remote/internal/account"
	"github.com/xallor-ai/xallor-remote/internal/admission"
	"github.com/xallor-ai/xallor-remote/internal/delegation"
	"github.com/xallor-ai/xallor-remote/internal/ipc"
	"github.com/xallor-ai/xallor-remote/internal/principal"
	"github.com/xallor-ai/xallor-remote/internal/protocol"
	"github.com/xallor-ai/xallor-remote/internal/relay"
	"github.com/xallor-ai/xallor-remote/internal/storage"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// Reuse the same independent owners, real IPC approval and seeded code for
// foreground and handoff acceptance; no production issuance endpoint is implied.
func newIndependentConsent(t *testing.T, scope []string) (*independentHarness, *storage.SQLStore, access.Device, accesscode.Code, independentSession, *admission.Service, string) {
	return newIndependentConsentWith(t, scope, 5*time.Minute, nil)
}

// Setup stops before requesting consent so protocol heads can drive the request.
func newIndependentConsentSetup(t *testing.T, scope []string, prepare func(*independentHarness)) (*independentHarness, *storage.SQLStore, access.Device, accesscode.Code, *admission.Service, string, *ipc.Conn) {
	t.Helper()
	scope = append([]string(nil), scope...)
	slices.Sort(scope)
	s, err := storage.OpenSQLStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Error(err)
		}
	})
	accounts, permissions := principal.NewService(s), access.NewService(s)
	key, err := s.ExecutionKey()
	if err != nil {
		t.Fatal(err)
	}
	capacity := admission.New(admission.Limits{PerDevice: protocol.MaxDeviceRequests})
	hub := relay.NewHub(s, accounts, permissions, delegation.NewAuthority(key, s, accounts, permissions), capacity, s)
	server := httptest.NewServer(relay.Handler(hub, nil))
	t.Cleanup(server.Close)
	url := "ws" + strings.TrimPrefix(server.URL, "http")
	originalPath := os.Getenv("PATH")
	h := newIndependentHarness(t, url)
	// Keep the controlled verifier first while allowing real Git preflight.
	t.Setenv("PATH", os.Getenv("PATH")+string(os.PathListSeparator)+originalPath)
	target, controller := h.profiles[0], h.profiles[1]
	owner, err := account.New(s).CreatePrincipal(context.Background(), account.CreatePrincipalRequest{Name: "exec-test-owner", Kind: "user", TTL: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	targetStore := configureIndependentProfile(t, target, url, owner.Token)
	if _, err := targetStore.IssueGrant(); err != nil {
		t.Fatal(err)
	}
	controllerStore := configureIndependentProfile(t, controller, url, owner.Token)
	if err := controllerStore.AddPeer(targetStore.ID(), ""); err != nil {
		t.Fatal(err)
	}
	if slices.Contains(scope, protocol.OpHandoffSubmit) {
		observer := &independentProfile{dir: filepath.Join(h.root, "observer")}
		h.profiles = append(h.profiles, observer)
		observerStore := configureIndependentProfile(t, observer, url, owner.Token)
		if err := observerStore.AddPeer(targetStore.ID(), ""); err != nil {
			t.Fatal(err)
		}
	}
	for _, p := range h.profiles {
		if err := h.start(p, true); err != nil {
			t.Fatal("Runtime install failed", err)
		}
	}
	deadline := time.Now().Add(10 * time.Second)
	for !hub.DeviceOnline(targetStore.ID()) {
		if time.Now().After(deadline) {
			t.Fatal("target Relay readiness missing")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if prepare != nil {
		prepare(h)
	}
	management, err := h.connect(target)
	if err != nil {
		t.Fatal(err)
	}
	f, err := independentCall(management, "claim", "device.access", map[string]any{"action": "claim"})
	requireIndependentOK(t, f, err)
	device, err := s.LoadDevice(context.Background(), targetStore.ID())
	if err != nil {
		t.Fatal(err)
	}
	if len(h.profiles) == 3 {
		observer, err := h.connect(h.profiles[2])
		if err != nil {
			t.Fatal(err)
		}
		// Warm only the owner's receipt-query path before the consent TTL starts.
		// Missing receipt is an application result, not permission to submit.
		frame, err := independentCall(observer, "observer-ready", protocol.OpHandoffGet, map[string]any{"device_id": targetStore.ID(), "request_id": "observer-prewarm"})
		requireIndependentOK(t, frame, err)
		_ = observer.Close()
		h.observations["observer_mode"] = "independent owner get only; no code redemption"
	}
	code := accesscode.Code{ID: "independent-exec-code", DeviceUID: device.UID, OwnershipID: device.OwnershipID, Generation: 1, Type: accesscode.LongLived, IssuedAt: time.Now().UTC(), Scope: append([]string(nil), scope...), RequireApproval: true, State: accesscode.StateActive}
	if err := s.WriteAccessCode(context.Background(), func(tx accesscode.Transaction) error {
		return tx.InsertCode(context.Background(), code, protocol.SHA256Hex("independent-exec-secret"))
	}); err != nil {
		t.Fatal(err)
	}
	return h, s, device, code, capacity, targetStore.ID(), management
}

func newIndependentConsentWith(t *testing.T, scope []string, ttl time.Duration, prepare func(*independentHarness)) (*independentHarness, *storage.SQLStore, access.Device, accesscode.Code, independentSession, *admission.Service, string) {
	t.Helper()
	h, s, device, code, capacity, targetID, management := newIndependentConsentSetup(t, scope, prepare)
	controller := h.profiles[1]
	var f ipc.Frame
	var err error
	f, err = independentCall(management, "subscribe", "session.approval.subscribe", map[string]any{})
	requireIndependentOK(t, f, err)
	requester, err := h.connect(controller)
	if err != nil {
		t.Fatal(err)
	}
	expires := time.Now().Add(ttl).Unix()
	if err := ipc.Call(requester, "session", "session.request", map[string]any{"device_id": targetID, "target_uid": device.UID, "code_id": code.ID, "code_secret": "independent-exec-secret", "generation": 1, "scope": code.Scope, "session_expires_at": expires}); err != nil {
		t.Fatal(err)
	}
	event, err := management.ReadFrame()
	if err != nil {
		t.Fatal(err)
	}
	var approval struct {
		CodeID string   `json:"code_id"`
		Scope  []string `json:"scope"`
		Digest string   `json:"request_sha256"`
	}
	if event.Event != "session_approval" || event.ID == "" || json.Unmarshal(event.Params, &approval) != nil || approval.CodeID != code.ID || !slices.Equal(approval.Scope, code.Scope) || len(approval.Digest) != 64 {
		t.Fatal("exec consent request invalid")
	}
	f, err = independentCall(management, "approve", "session.approval.respond", map[string]any{"approval_id": event.ID, "request_sha256": approval.Digest, "allow": true})
	requireIndependentOK(t, f, err)
	f, err = requester.ReadFrame()
	requireIndependentOK(t, f, err)
	var session independentSession
	if f.ID != "session" || json.Unmarshal(f.Result, &session) != nil || session.SessionID == "" || session.Status != protocol.SessionActive || !slices.Equal(session.Scope, code.Scope) || session.ExpiresAt == nil || *session.ExpiresAt != expires {
		t.Fatal("exec session not approved")
	}
	return h, s, device, code, session, capacity, targetID
}
