package cli

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/xallor-ai/xallor-remote/internal/accesscode"
	"github.com/xallor-ai/xallor-remote/internal/execution"
	"github.com/xallor-ai/xallor-remote/internal/protocol"
)

// The shipped MCP entry runs as a real Node stdio child, with two independent
// Runtime owners. Recipient IPC approval and owner MCP revoke control authority.
func TestIndependentMCP_shouldRequestScopedConsentAndRejectAfterOwnerRevoke(t *testing.T) {
	node, entry := independentMCPPaths(t)
	h, store, device, code, capacity, targetID, management := newIndependentConsentSetup(t, []string{protocol.OpInfo, protocol.OpRead, protocol.OpExec}, nil)
	target, controller := h.profiles[0], h.profiles[1]
	if err := os.MkdirAll(filepath.Join(target.dir, "workspace"), 0700); err != nil {
		t.Fatal(err)
	}
	const contents = "mcp-consent-读🌍\n"
	if err := os.WriteFile(filepath.Join(target.dir, "workspace", "approved.txt"), []byte(contents), 0600); err != nil {
		t.Fatal(err)
	}
	management.Close()
	client := h.startMCP(controller, node, entry)
	client.sameOwner(controller)
	var catalog struct {
		Tools []struct {
			Name string `json:"name"`
		} `json:"tools"`
	}
	if json.Unmarshal(client.receive(client.send("tools/list", map[string]any{})), &catalog) != nil {
		t.Fatal("MCP catalog invalid")
	}
	for _, name := range []string{"remote.session_request", "remote.device_code_revoke", "remote.device_info", "remote.exec", "remote.read", "remote.write"} {
		found := false
		for _, tool := range catalog.Tools {
			found = found || tool.Name == name
		}
		if !found {
			t.Fatal("missing MCP tool", name)
		}
	}
	// Node initialization has its own deadline; only start the recipient's
	// existing IPC deadline once initialization/catalog admission is finished.
	management, err := h.connect(target)
	if err != nil {
		t.Fatal(err)
	}
	f, err := independentCall(management, "mcp-subscribe", "session.approval.subscribe", map[string]any{})
	requireIndependentOK(t, f, err)
	expires := time.Now().Add(5 * time.Minute).Unix()
	id := client.send("tools/call", map[string]any{"name": "remote.session_request", "arguments": map[string]any{
		"device_id": targetID, "target_uid": device.UID, "code_id": code.ID,
		"generation": code.Generation, "scope": code.Scope, "session_expires_at": expires,
	}})
	event, err := management.ReadFrame()
	var approval struct {
		CodeID string   `json:"code_id"`
		Scope  []string `json:"scope"`
		Digest string   `json:"request_sha256"`
	}
	if err != nil || event.Event != "session_approval" || event.ID == "" || json.Unmarshal(event.Params, &approval) != nil || approval.CodeID != code.ID || !slices.Equal(approval.Scope, code.Scope) || len(approval.Digest) != 64 {
		t.Fatal("MCP request did not reach recipient approval with exact scope")
	}
	f, err = independentCall(management, "mcp-approve", "session.approval.respond", map[string]any{"approval_id": event.ID, "request_sha256": approval.Digest, "allow": true})
	requireIndependentOK(t, f, err)
	var session independentSession
	if json.Unmarshal([]byte(client.success(client.toolResult(id))), &session) != nil || session.SessionID == "" || session.Status != protocol.SessionActive || !slices.Equal(session.Scope, code.Scope) || session.ExpiresAt == nil || *session.ExpiresAt != expires || session.ResumeID == "" || session.Capability == "" {
		t.Fatal("MCP changed approved consent or omitted resume proof")
	}
	h.cases = append(h.cases, "mcp_stdio_catalog", "mcp_request_recipient_ipc_approval")
	var info struct {
		DeviceID string `json:"device_id"`
	}
	if json.Unmarshal([]byte(client.success(client.tool("remote.device_info", map[string]any{"device_id": targetID, "session_id": session.SessionID}))), &info) != nil || info.DeviceID != targetID {
		t.Fatal("MCP info did not come from target")
	}
	read := client.success(client.tool("remote.read", map[string]any{"device_id": targetID, "session_id": session.SessionID, "path": "approved.txt"}))
	if !strings.Contains(read, contents) {
		t.Fatal("MCP read lost approved UTF-8 contents")
	}
	client.denied(client.tool("remote.write", map[string]any{"device_id": targetID, "session_id": session.SessionID, "path": "unapproved.txt", "content": "must-not-write"}), protocol.Unauthorized)
	if _, err := os.Stat(filepath.Join(target.dir, "workspace", "unapproved.txt")); !os.IsNotExist(err) {
		t.Fatal("unapproved MCP write created a file")
	}
	program := execution.Program{Executable: filepath.Join(h.bundle, h.manifest.Artifact.Filename), Args: []string{"version", "--json"}}
	executed := client.success(client.tool("remote.exec", map[string]any{"device_id": targetID, "session_id": session.SessionID, "program": program}))
	if !strings.Contains(executed, "status=completed exit_code=0") || !strings.Contains(executed, strings.Repeat("a", 40)) {
		t.Fatal("MCP approved native exec result missing")
	}
	if len(readIndependentJobFile(t, target.dir)) != 0 || capacity.Active(device.UID) != 0 {
		t.Fatal("MCP completed execution retained occupancy")
	}
	h.cases = append(h.cases, "mcp_scoped_info_read_exec", "mcp_unapproved_write_denied_without_file")
	owner := h.startMCP(target, node, entry)
	owner.sameOwner(target)
	owner.success(owner.tool("remote.device_code_revoke", map[string]any{"code_id": code.ID}))
	client.denied(client.tool("remote.device_info", map[string]any{"device_id": targetID, "session_id": session.SessionID}), protocol.SessionPending)
	client.denied(client.tool("remote.device_info", map[string]any{"device_id": targetID}), protocol.SessionPending)
	client.denied(client.tool("remote.exec", map[string]any{"device_id": targetID, "session_id": session.SessionID, "program": program}), protocol.SessionPending)
	if err := store.ReadAccessCode(context.Background(), func(r accesscode.Reader) error {
		stored, err := r.Session(context.Background(), session.SessionID)
		if err == nil && stored.State != accesscode.SessionRevoked {
			t.Error("MCP owner revoke not durable")
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	client.sameOwner(controller)
	owner.sameOwner(target)
	for _, p := range h.profiles {
		if exited, err := p.current.watch.Exited(); err != nil || exited {
			t.Fatal("MCP operation stopped Runtime owner", err)
		}
	}
	h.cases = append(h.cases, "mcp_owner_revoke", "mcp_revoked_exec_info_denied", "mcp_no_account_fallback", "mcp_same_runtime_owners")
	h.observations["mcp_transport"] = "real Node child stdio JSON-RPC"
	h.observations["approved_scope"] = code.Scope
	h.observations["jobs_after_terminal"] = 0
}
