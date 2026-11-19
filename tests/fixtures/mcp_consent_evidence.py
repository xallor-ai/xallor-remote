"""Acceptance gate: never substitute a skip or partial cleanup for MCP E2E."""

CASE = "TestIndependentMCP_shouldRequestScopedConsentAndRejectAfterOwnerRevoke"
PACKAGE = "github.com/xallor-ai/xallor-remote/internal/cli"
CASES = {
    "mcp_stdio_catalog", "mcp_request_recipient_ipc_approval",
    "mcp_scoped_info_read_exec", "mcp_unapproved_write_denied_without_file",
    "mcp_owner_revoke", "mcp_revoked_exec_info_denied",
    "mcp_no_account_fallback", "mcp_same_runtime_owners",
}
RESUME = "TestIndependentMCP_shouldResumeOnceAfterRuntimeRestart"
REVOKE = "TestIndependentMCP_shouldReapRunningTaskTreeAfterOwnerRevoke"
ROTATE = "TestIndependentMCP_shouldReapRunningTaskTreeAfterOwnerRotate"
RESUME_CASES = {"mcp_controller_runtime_replaced", "mcp_reconnect_requires_resume", "mcp_wrong_resume_denied",
                "mcp_same_consent_resumed", "mcp_consumed_proof_reuse_denied", "mcp_resume_scope_preserved_no_write", "mcp_revoked_resume_denied"}
MANAGEMENT_CASES = {"mcp_progress_live_task_tree", "mcp_job_transport_occupied", "mcp_authorization_lost_preserves_output",
                    "mcp_kernel_task_tree_exit", "mcp_job_transport_released", "mcp_old_session_execution_denied", "mcp_same_runtime_owners"}
SCENARIOS = {
    CASE: (CASES, ["exec", "info", "read"], 2, 2),
    RESUME: (RESUME_CASES, ["info"], 3, 4),
    REVOKE: (MANAGEMENT_CASES | {"code.revoke"}, ["exec"], 2, 2),
    ROTATE: (MANAGEMENT_CASES | {"code.rotate"}, ["exec"], 2, 2),
}


def validate(events, report, case=CASE):
    if case not in SCENARIOS:
        raise ValueError("unknown MCP acceptance case")
    expected, scope, owner_count, head_count = SCENARIOS[case]
    actions = [event.get("Action") for event in events
               if event.get("Test") == case and event.get("Package") == PACKAGE
               and event.get("Action") != "output"]
    if actions != ["run", "pass"] or any(e.get("Action") == "fail" for e in events):
        raise ValueError("exact MCP acceptance case did not run and pass")
    if (report.get("passed") is not True or report.get("profile_root_removed") is not True
            or report.get("cleanup_errors") != [] or report.get("signed_release") is not False):
        raise ValueError("inner acceptance or cleanup incomplete")
    cases = report.get("cases", [])
    if len(cases) != len(expected) or set(cases) != expected:
        raise ValueError("MCP acceptance coverage incomplete")
    owners = report.get("launches", [])
    heads = report.get("observations", {}).get("mcp_processes", [])
    if len(owners) != owner_count or len(heads) != head_count:
        raise ValueError("independent process count wrong")
    if not all(all(row.get(key) is True for key in ("kernel_exit", "reaped", "endpoint_absent")) for row in owners):
        raise ValueError("Runtime owner cleanup incomplete")
    if not all(row.get("reaped") is True and row.get("cleanup_confirmed") is True for row in heads):
        raise ValueError("MCP head cleanup incomplete")
    # The old controller in the restart case is no longer live; its PID can
    # legitimately be reused later. Only simultaneously live identities differ.
    pids = [row.get("pid") for row in [owners[0], owners[-1]] + heads]
    if not all(type(pid) is int and pid > 0 for pid in pids) or len(set(pids)) != 2 + head_count:
        raise ValueError("expected distinct live process identities")
    observations = report.get("observations", {})
    if observations.get("mcp_transport") != "real Node child stdio JSON-RPC" or observations.get("approved_scope") != scope:
        raise ValueError("MCP transport, scope or occupancy evidence incomplete")
    if case == RESUME:
        old, new = owners[1], owners[2]
        if (observations.get("resume_proof_rotated") is not True or observations.get("old_controller_pid") != old["pid"]
                or observations.get("new_controller_pid") != new["pid"] or old["pid"] == new["pid"]
                or not old.get("launch_id") or not new.get("launch_id") or old["launch_id"] == new["launch_id"]
                or not old.get("image_sha256") or old["image_sha256"] != new.get("image_sha256")):
            raise ValueError("Runtime replacement or resume proof evidence missing")
        return
    if observations.get("jobs_after_terminal") != 0:
        raise ValueError("MCP job release missing")
    if case in (REVOKE, ROTATE):
        action = "code.revoke" if case == REVOKE else "code.rotate"
        tasks = observations.get("task_processes", {})
        if (observations.get("management_action") != action or observations.get("mcp_progress_seen") is not True
                or observations.get("terminal_status") != "authorization_lost" or observations.get("transport_after_terminal") != 0
                or set(tasks) != {"root", "leaf"}):
            raise ValueError("MCP management terminal evidence incomplete")
        for role in ("root", "leaf"):
            task = tasks[role]
            if task.get("role") != role or not all(task.get(k) is True for k in ("image_bound", "initially_live", "exited_at_terminal", "exited_at_cleanup")):
                raise ValueError("MCP task lifecycle evidence incomplete")
            pid = task.get("pid")
            if type(pid) is not int or pid <= 0 or pid in pids:
                raise ValueError("MCP task identity conflicts")
            pids.append(pid)
        if tasks["root"].get("parent_pid") != owners[0]["pid"] or tasks["leaf"].get("parent_pid") != tasks["root"]["pid"]:
            raise ValueError("MCP task ancestry metadata mismatch")
