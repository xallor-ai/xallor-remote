package cli

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/xallor-ai/xallor-remote/internal/accesscode"
	"github.com/xallor-ai/xallor-remote/internal/protocol"
)

// The controller Runtime is really stopped/reaped and replaced. MCP resume must
// retain consent, rotate its one-use proof, and refuse wrong/reused/revoked proof.
func TestIndependentMCP_shouldResumeOnceAfterRuntimeRestart(t *testing.T) {
	node, entry := independentMCPPaths(t)
	h, store, device, code, session, _, targetID := newIndependentConsent(t, []string{protocol.OpInfo})
	target, controller := h.profiles[0], h.profiles[1]
	original := h.startMCP(controller, node, entry)
	original.sameOwner(controller)
	assertMCPInfo(t, original, targetID, session.SessionID)
	old := controller.current
	if err := h.stop(controller); err != nil {
		t.Fatal(err)
	}
	if err := h.start(controller, false); err != nil {
		t.Fatal(err)
	}
	if !old.KernelExit || !old.Reaped || !old.EndpointAbsent || old.PID == controller.current.PID || old.Launch == controller.current.Launch || old.SHA256 != controller.current.SHA256 {
		t.Fatal("controller was not replaced with the same admitted image")
	}
	// Existing MCP re-observes the replacement owner; it does not replay work.
	original.sameOwner(controller)
	// A fresh Relay connection has not entered session mode: explicitly naming
	// an old session is unauthorized until a resume proof binds this connection.
	original.denied(original.tool("remote.device_info", map[string]any{"device_id": targetID, "session_id": session.SessionID}), protocol.Unauthorized)
	args := map[string]any{"device_id": targetID, "target_uid": device.UID, "session_id": session.SessionID, "resume_id": session.ResumeID}
	wrong := h.startMCP(controller, node, entry, independentMCPOptions{resumeCapability: "wrong-capability"})
	wrong.denied(wrong.tool("remote.session_resume", args), protocol.Unauthorized)
	client := h.startMCP(controller, node, entry, independentMCPOptions{resumeCapability: session.Capability})
	var resumed independentSession
	if json.Unmarshal([]byte(client.success(client.tool("remote.session_resume", args))), &resumed) != nil || resumed.Status != protocol.SessionActive || resumed.SessionID != session.SessionID || !slices.Equal(resumed.Scope, session.Scope) || resumed.ExpiresAt == nil || *resumed.ExpiresAt != *session.ExpiresAt || resumed.ResumeID == "" || resumed.ResumeID == session.ResumeID || resumed.Capability == "" || resumed.Capability == session.Capability {
		t.Fatal("MCP resume changed consent or failed to rotate proof")
	}
	if err := store.ReadAccessCode(context.Background(), func(r accesscode.Reader) error {
		proof, err := r.Resume(context.Background(), resumed.ResumeID)
		if err == nil && (proof.State != accesscode.ResumeActive || proof.SessionID != session.SessionID || proof.CapabilityHash != protocol.SHA256Hex(resumed.Capability)) {
			t.Error("new MCP resume proof not durable and active")
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	client.denied(client.tool("remote.session_resume", args), protocol.Unauthorized)
	assertMCPInfo(t, client, targetID, resumed.SessionID)
	client.denied(client.tool("remote.write", map[string]any{"device_id": targetID, "session_id": resumed.SessionID, "path": "resume-unapproved.txt", "content": "must-not-write"}), protocol.Unauthorized)
	if _, err := os.Stat(filepath.Join(target.dir, "workspace", "resume-unapproved.txt")); !os.IsNotExist(err) {
		t.Fatal("resume widened write scope")
	}
	management, err := h.connect(target)
	if err != nil {
		t.Fatal(err)
	}
	f, err := independentCall(management, "revoke-resumed", "device.access", map[string]any{"action": "code.revoke", "code_id": code.ID})
	requireIndependentOK(t, f, err)
	client.denied(client.tool("remote.device_info", map[string]any{"device_id": targetID, "session_id": resumed.SessionID}), protocol.SessionPending)
	args["resume_id"] = resumed.ResumeID
	revoked := h.startMCP(controller, node, entry, independentMCPOptions{resumeCapability: resumed.Capability})
	revoked.denied(revoked.tool("remote.session_resume", args), protocol.Unauthorized)
	client.sameOwner(controller)
	if exited, err := target.current.watch.Exited(); err != nil || exited {
		t.Fatal("target did not survive controller restart", err)
	}
	h.cases = append(h.cases, "mcp_controller_runtime_replaced", "mcp_reconnect_requires_resume", "mcp_wrong_resume_denied", "mcp_same_consent_resumed", "mcp_consumed_proof_reuse_denied", "mcp_resume_scope_preserved_no_write", "mcp_revoked_resume_denied")
	h.observations["mcp_transport"] = "real Node child stdio JSON-RPC"
	h.observations["approved_scope"] = code.Scope
	h.observations["old_controller_pid"] = old.PID
	h.observations["new_controller_pid"] = controller.current.PID
	h.observations["resume_proof_rotated"] = true
}

func assertMCPInfo(t *testing.T, client *independentMCPClient, device, session string) {
	t.Helper()
	var info struct {
		DeviceID string `json:"device_id"`
	}
	if json.Unmarshal([]byte(client.success(client.tool("remote.device_info", map[string]any{"device_id": device, "session_id": session}))), &info) != nil || info.DeviceID != device {
		t.Fatal("MCP info did not come from approved target")
	}
}
