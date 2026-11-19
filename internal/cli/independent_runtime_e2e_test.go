package cli

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/xallor-ai/xallor-remote/internal/access"
	"github.com/xallor-ai/xallor-remote/internal/accesscode"
	"github.com/xallor-ai/xallor-remote/internal/account"
	"github.com/xallor-ai/xallor-remote/internal/admission"
	"github.com/xallor-ai/xallor-remote/internal/identity"
	"github.com/xallor-ai/xallor-remote/internal/ipc"
	"github.com/xallor-ai/xallor-remote/internal/principal"
	"github.com/xallor-ai/xallor-remote/internal/protocol"
	"github.com/xallor-ai/xallor-remote/internal/relay"
	"github.com/xallor-ai/xallor-remote/internal/storage"
)

// Install two independent owners through the real permit/selection flow.
// Named-pipe/socket peer PIDs must match, and stopping one leaves the other live.
func TestIndependentRuntime_shouldIsolateProfilesAndIPCProcesses(t *testing.T) {
	h := newIndependentHarness(t, "ws://127.0.0.1:1/remote")
	for _, p := range h.profiles {
		if err := h.start(p, true); err != nil {
			t.Fatal("owner startup", err)
		}
	}
	a, b := h.profiles[0], h.profiles[1]
	if a.current.PID == b.current.PID || a.endpoint == b.endpoint {
		t.Fatal("owners share PID or IPC endpoint")
	}
	for _, p := range h.profiles {
		c, err := h.connect(p)
		if err != nil {
			t.Fatal(err)
		}
		f, err := independentCall(c, "status", "status", map[string]any{})
		requireIndependentOK(t, f, err)
		c.Close()
		if exited, err := p.current.watch.Exited(); err != nil || exited {
			t.Fatal("owner not alive", err)
		}
	}
	h.cases = append(h.cases, "distinct_live_owner_pids_and_ipc_peers")
	if err := h.stop(b); err != nil {
		t.Fatal(err)
	}
	if exited, err := a.current.watch.Exited(); err != nil || exited {
		t.Fatal("stopping client stopped target", err)
	}
	c, err := h.connect(a)
	if err != nil {
		t.Fatal(err)
	}
	f, err := independentCall(c, "surviving-status", "status", map[string]any{})
	requireIndependentOK(t, f, err)
	c.Close()
	h.cases = append(h.cases, "stopping_one_preserves_other")
	if err := h.stop(a); err != nil {
		t.Fatal(err)
	}
}

// Independent runtime-serve processes perform approval, a scoped remote call,
// actual controller process replacement/resume and owner revocation via OS IPC.
// The parent owns a real loopback Relay/SQLite; issuance is seeded, not a product
// issuing UI/CLI. Source verification uses the established native gh fixture.
func TestIndependentRuntime_shouldApproveResumeAndRevoke_overSeparateProcesses(t *testing.T) {
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
	hub := relay.NewHub(s, accounts, permissions, nil, admission.New(admission.Limits{PerDevice: protocol.MaxDeviceRequests}), s)
	server := httptest.NewServer(relay.Handler(hub, nil))
	t.Cleanup(server.Close)
	url := "ws" + strings.TrimPrefix(server.URL, "http")
	h := newIndependentHarness(t, url)
	target, client := h.profiles[0], h.profiles[1]
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	owner, err := account.New(s).CreatePrincipal(ctx, account.CreatePrincipalRequest{Name: "isolated-test-owner", Kind: "user", TTL: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	targetStore := configureIndependentProfile(t, target, url, owner.Token)
	if _, err := targetStore.IssueGrant(); err != nil {
		t.Fatal(err)
	}
	clientStore := configureIndependentProfile(t, client, url, owner.Token)
	if err := clientStore.AddPeer(targetStore.ID(), ""); err != nil {
		t.Fatal(err)
	}
	if err := h.start(target, true); err != nil {
		t.Fatal("target install", err)
	}
	if err := h.start(client, true); err != nil {
		t.Fatal("client install", err)
	}
	if target.current.PID == client.current.PID || target.endpoint == client.endpoint {
		t.Fatal("owners were not distinct")
	}
	waitCtx, stopWait := context.WithTimeout(context.Background(), 10*time.Second)
	defer stopWait()
	for !hub.DeviceOnline(targetStore.ID()) {
		select {
		case <-waitCtx.Done():
			t.Fatal("target did not confirm Relay settings")
		case <-time.After(10 * time.Millisecond):
		}
	}
	approvalConn, err := h.connect(target)
	if err != nil {
		t.Fatal(err)
	}
	f, err := independentCall(approvalConn, "claim", "device.access", map[string]any{"action": "claim"})
	requireIndependentOK(t, f, err)
	device, err := s.LoadDevice(context.Background(), targetStore.ID())
	if err != nil {
		t.Fatal(err)
	}
	code := accesscode.Code{ID: "independent-code", DeviceUID: device.UID, OwnershipID: device.OwnershipID, Type: accesscode.OneTime, Generation: 1, IssuedAt: time.Now().UTC(), Scope: []string{protocol.OpInfo, protocol.OpWrite}, RequireApproval: true, State: accesscode.StateActive}
	if err := s.WriteAccessCode(context.Background(), func(tx accesscode.Transaction) error {
		return tx.InsertCode(context.Background(), code, protocol.SHA256Hex("isolated-code-secret"))
	}); err != nil {
		t.Fatal(err)
	}
	f, err = independentCall(approvalConn, "subscribe", "session.approval.subscribe", map[string]any{})
	requireIndependentOK(t, f, err)
	caller, err := h.connect(client)
	if err != nil {
		t.Fatal(err)
	}
	if err := ipc.Call(caller, "request", "session.request", map[string]any{"device_id": targetStore.ID(), "code_id": code.ID, "code_secret": "isolated-code-secret", "target_uid": device.UID, "generation": 1, "scope": []string{protocol.OpInfo}, "session_expires_at": time.Now().Add(5 * time.Minute).Unix()}); err != nil {
		t.Fatal(err)
	}
	event, err := approvalConn.ReadFrame()
	if err != nil {
		t.Fatal(err)
	}
	var approval struct {
		CodeID string   `json:"code_id"`
		Scope  []string `json:"scope"`
		Digest string   `json:"request_sha256"`
	}
	if event.Event != "session_approval" || event.ID == "" || json.Unmarshal(event.Params, &approval) != nil || approval.CodeID != code.ID || len(approval.Scope) != 1 || approval.Scope[0] != protocol.OpInfo || len(approval.Digest) != 64 {
		t.Fatal("target approval was not scoped to this request")
	}
	f, err = independentCall(approvalConn, "approve", "session.approval.respond", map[string]any{"approval_id": event.ID, "request_sha256": approval.Digest, "allow": true})
	requireIndependentOK(t, f, err)
	f, err = caller.ReadFrame()
	requireIndependentOK(t, f, err)
	if f.ID != "request" {
		t.Fatal("session result correlation changed")
	}
	var session independentSession
	if json.Unmarshal(f.Result, &session) != nil || session.Status != protocol.SessionActive || session.SessionID == "" || session.ResumeID == "" || session.Capability == "" || len(session.Scope) != 1 || session.Scope[0] != protocol.OpInfo {
		t.Fatal("session metadata missing")
	}
	assertIndependentInfo(t, caller, targetStore.ID(), session.SessionID)
	h.cases = append(h.cases, "owner_claim_over_ipc", "recipient_approval_over_ipc", "scoped_remote_info")
	if err := caller.Close(); err != nil {
		t.Fatal(err)
	}
	old := client.current
	if err := h.stop(client); err != nil {
		t.Fatal("controller stop", err)
	}
	if err := h.start(client, false); err != nil {
		t.Fatal("controller restart", err)
	}
	if client.current.PID == old.PID || client.current.Launch == old.Launch || !old.KernelExit || !old.Reaped {
		t.Fatal("controller was not replaced after confirmed exit")
	}
	if exited, err := target.current.watch.Exited(); err != nil || exited {
		t.Fatal("target did not survive controller restart", err)
	}
	caller, err = h.connect(client)
	if err != nil {
		t.Fatal(err)
	}
	resume := map[string]any{"device_id": targetStore.ID(), "session_id": session.SessionID, "resume_id": session.ResumeID, "resume_capability": "wrong-capability", "target_uid": device.UID}
	f, err = independentCall(caller, "wrong", "session.resume", resume)
	if err != nil || f.OK == nil || *f.OK || f.Code != protocol.Unauthorized {
		t.Fatal("wrong resume capability not denied", err)
	}
	resume["resume_capability"] = session.Capability
	f, err = independentCall(caller, "resume", "session.resume", resume)
	requireIndependentOK(t, f, err)
	var resumed independentSession
	if json.Unmarshal(f.Result, &resumed) != nil || resumed.Status != protocol.SessionActive || resumed.Capability == "" || resumed.Capability == session.Capability || resumed.SessionID != session.SessionID || resumed.ResumeID == session.ResumeID || resumed.ResumeID == "" || len(resumed.Scope) != 1 || resumed.Scope[0] != protocol.OpInfo {
		t.Fatal("restart resume changed consent or failed to rotate proof")
	}
	assertIndependentInfo(t, caller, targetStore.ID(), resumed.SessionID)
	f, err = independentCall(caller, "reuse", "session.resume", resume)
	if err != nil || f.OK == nil || *f.OK || f.Code != protocol.Unauthorized {
		t.Fatal("consumed resume proof reused", err)
	}
	f, err = independentCall(caller, "outside-scope", "write", map[string]any{"device_id": targetStore.ID(), "session_id": resumed.SessionID, "path": "unapproved.txt", "content": "must-not-write"})
	if err != nil || f.OK == nil || *f.OK || f.Code != protocol.Unauthorized {
		t.Fatal("resume widened approved scope", err)
	}
	if _, err := os.Stat(filepath.Join(target.dir, "workspace", "unapproved.txt")); !os.IsNotExist(err) {
		t.Fatal("unapproved write reached target")
	}
	h.cases = append(h.cases, "controller_process_restart", "wrong_resume_denied", "same_consent_resumed", "consumed_proof_reuse_denied", "scope_preserved_no_write")
	if err := s.ReadAccessCode(context.Background(), func(r accesscode.Reader) error {
		proof, err := r.Resume(context.Background(), resumed.ResumeID)
		if err == nil && (proof.State != accesscode.ResumeActive || proof.SessionID != session.SessionID || proof.CapabilityHash != protocol.SHA256Hex(resumed.Capability)) {
			t.Error("new resume proof not active/bound before revocation")
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	management, err := h.connect(target)
	if err != nil {
		t.Fatal(err)
	}
	f, err = independentCall(management, "revoke", "device.access", map[string]any{"action": "code.revoke", "code_id": code.ID})
	requireIndependentOK(t, f, err)
	f, err = independentCall(caller, "revoked-info", "info", map[string]any{"device_id": targetStore.ID(), "session_id": resumed.SessionID})
	if err != nil || f.OK == nil || *f.OK || f.Code != protocol.SessionPending {
		t.Fatalf("revoked invoke expected denial: ok=%v code=%s error=%v", f.OK != nil && *f.OK, f.Code, err)
	}
	// Detached consent stays in session mode; it cannot fall back to the
	// account's transport permissions or reuse the rotated resume proof.
	f, err = independentCall(caller, "no-fallback", "info", map[string]any{"device_id": targetStore.ID()})
	if err != nil || f.OK == nil || *f.OK || f.Code != protocol.SessionPending {
		t.Fatal("revocation restored transport fallback", err)
	}
	resume["resume_id"], resume["resume_capability"] = resumed.ResumeID, resumed.Capability
	f, err = independentCall(caller, "revoked-resume", "session.resume", resume)
	if err != nil || f.OK == nil || *f.OK || f.Code != protocol.Unauthorized {
		t.Fatal("revoked proof restored consent", err)
	}
	if err := s.ReadAccessCode(context.Background(), func(r accesscode.Reader) error {
		current, err := r.Session(context.Background(), resumed.SessionID)
		if err == nil && current.State != accesscode.SessionRevoked {
			t.Error("owner revocation was not durable")
		}
		if err != nil {
			return err
		}
		proof, err := r.Resume(context.Background(), resumed.ResumeID)
		if err == nil && proof.State != accesscode.ResumeRevoked {
			t.Error("previously active resume proof not durably revoked")
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	h.cases = append(h.cases, "owner_revoke_over_target_ipc", "revoked_session_invoke_denied", "no_grant_fallback", "revoked_resume_denied")
}

type independentSession struct {
	ExpiresAt  *int64   `json:"session_expires_at"`
	SessionID  string   `json:"session_id"`
	Status     string   `json:"status"`
	ResumeID   string   `json:"resume_id"`
	Capability string   `json:"resume_capability"`
	Scope      []string `json:"scope"`
}

func configureIndependentProfile(t *testing.T, p *independentProfile, url, token string) *identity.Store {
	t.Helper()
	// Persist before launching; child connections and credentials use only this profile.
	s, err := identity.LoadFrom(p.dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetConfig(url, filepath.Join(p.dir, "workspace"), "auto"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetAccount(token); err != nil {
		t.Fatal(err)
	}
	return s
}

func assertIndependentInfo(t *testing.T, c *ipc.Conn, deviceID, sessionID string) {
	t.Helper()
	f, err := independentCall(c, "info", "info", map[string]any{"device_id": deviceID, "session_id": sessionID})
	requireIndependentOK(t, f, err)
	var terminal struct {
		Status  string `json:"status"`
		Exit    *int   `json:"exit_code"`
		Content string `json:"content"`
	}
	if json.Unmarshal(f.Result, &terminal) != nil || terminal.Status != protocol.ExitCompleted || terminal.Exit == nil || *terminal.Exit != 0 {
		t.Fatal("remote info terminal not successful")
	}
	var info struct {
		DeviceID string `json:"device_id"`
	}
	if json.Unmarshal([]byte(terminal.Content), &info) != nil || info.DeviceID != deviceID {
		t.Fatal("info was not produced by target profile")
	}
}
