package cli

import (
	"encoding/json"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/xallor-ai/xallor-remote/internal/access"
	"github.com/xallor-ai/xallor-remote/internal/accesscode"
	"github.com/xallor-ai/xallor-remote/internal/execution"
	"github.com/xallor-ai/xallor-remote/internal/protocol"
)

// Revoke through the target's real MCP while controller MCP owns a live task tree.
func TestIndependentMCP_shouldReapRunningTaskTreeAfterOwnerRevoke(t *testing.T) {
	assertIndependentMCPManagement(t, "code.revoke")
}

// Rotation must stop the old session's task; replacement metadata is not consent.
func TestIndependentMCP_shouldReapRunningTaskTreeAfterOwnerRotate(t *testing.T) {
	assertIndependentMCPManagement(t, "code.rotate")
}

func assertIndependentMCPManagement(t *testing.T, action string) {
	t.Helper()
	node, entry := independentMCPPaths(t)
	h, _, device, code, session, capacity, targetID := newIndependentConsent(t, []string{protocol.OpExec})
	target, controller := h.profiles[0], h.profiles[1]
	client := h.startMCP(controller, node, entry)
	owner := h.startMCP(target, node, entry, independentMCPOptions{rotationSecret: "private-mcp-replacement-secret"})
	client.sameOwner(controller)
	owner.sameOwner(target)
	executable := filepath.Join(h.bundle, h.manifest.Artifact.Filename)
	program := execution.Program{Executable: executable, Args: []string{"--xr-independent-exec-root"}}
	const token = "independent-mcp-running-task"
	id := client.send("tools/call", map[string]any{"name": "remote.exec", "arguments": map[string]any{"device_id": targetID, "session_id": session.SessionID, "program": program}, "_meta": map[string]any{"progressToken": token}})
	tasks, watches, execID := h.observeMCPTasks(client, token, executable)
	if len(readIndependentJobFile(t, target.dir)) != 1 || capacity.Active(device.UID) != 1 {
		t.Fatal("MCP live task occupancy missing")
	}
	if action == "code.revoke" {
		owner.success(owner.tool("remote.device_code_revoke", map[string]any{"code_id": code.ID}))
	} else {
		text := owner.success(owner.tool("remote.device_code_rotate", map[string]any{"code_id": code.ID, "new_code_id": "mcp-replacement"}))
		var receipt access.CodeRotationResult
		if json.Unmarshal([]byte(text), &receipt) != nil || receipt.PreviousCodeID != code.ID || receipt.Code.ID != "mcp-replacement" || receipt.Code.Generation != 2 || receipt.Code.State != accesscode.StateActive || !receipt.Code.RequireApproval || strings.Contains(text, "private-mcp-replacement-secret") || strings.Contains(text, "intent_sha256") {
			t.Fatal("MCP rotation lost metadata or exposed private material")
		}
	}
	terminal := client.toolResult(id)
	text := terminal.Content[0].Text
	fields := regexp.MustCompile(`(?m)^status=authorization_lost exit_code=(-?[0-9]+) duration_ms=([0-9]+) truncated=(true|false) exec_id=(\S+)$`).FindStringSubmatch(text)
	if !terminal.IsError || len(fields) != 5 || fields[4] != execID || !strings.Contains(text, `"role":"root"`) || !strings.Contains(text, `"role":"leaf"`) {
		t.Fatal("MCP lost correlated authorization terminal or prior output")
	}
	duration, err := strconv.ParseInt(fields[2], 10, 64)
	if err != nil || duration >= 15000 {
		t.Fatal("MCP task did not settle within observation budget")
	}
	for role, watch := range watches {
		exited, err := watch.Exited()
		tasks[role].ExitedAtTerminal = exited && err == nil
		if err != nil || !exited {
			t.Fatal("MCP terminal observed before task exit", role, err)
		}
	}
	if len(readIndependentJobFile(t, target.dir)) != 0 || capacity.Active(device.UID) != 0 {
		t.Fatal("MCP terminal retained task/transport occupancy")
	}
	client.denied(client.tool("remote.exec", map[string]any{"device_id": targetID, "session_id": session.SessionID, "program": program}), protocol.SessionPending)
	client.sameOwner(controller)
	owner.sameOwner(target)
	for _, p := range h.profiles {
		if exited, err := p.current.watch.Exited(); err != nil || exited {
			t.Fatal("MCP management stopped Runtime owner", err)
		}
	}
	h.cases = append(h.cases, "mcp_progress_live_task_tree", "mcp_job_transport_occupied", action, "mcp_authorization_lost_preserves_output", "mcp_kernel_task_tree_exit", "mcp_job_transport_released", "mcp_old_session_execution_denied", "mcp_same_runtime_owners")
	h.observations["mcp_transport"] = "real Node child stdio JSON-RPC"
	h.observations["approved_scope"] = code.Scope
	h.observations["management_action"] = action
	h.observations["terminal_status"] = protocol.ExitAuthorizationLost
	h.observations["jobs_after_terminal"] = 0
	h.observations["transport_after_terminal"] = 0
}
