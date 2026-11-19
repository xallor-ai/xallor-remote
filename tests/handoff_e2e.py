"""Handoff state/dedupe/recovery over a real Relay and two Runtimes."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import subprocess
import sys
import time
import uuid
from fixtures.mechanics_cli import cluster_from_options, completed_report
from fixtures.handoff_cli import add_handoff_options, prepare_agent
from fixtures.process_ref import ProcessRef

sys.stdout.reconfigure(encoding="utf-8", errors="replace")
repo = Path(__file__).resolve().parents[1]
parser = argparse.ArgumentParser()
add_handoff_options(parser)
parser.add_argument("--output", default=str(repo/"dist/verification/handoff.json"))
args = parser.parse_args()
output = Path(args.output).resolve()
cluster = cluster_from_options(args, output, __file__)
references, results = [], []

def passed(name):
    results.append({"name":name,"status":"pass"})
    print(name,flush=True)

try:
    cluster.start()
    controller, target = cluster.add("controller"), cluster.add("target")
    owners = {name: json.loads(cluster.cli(name, "status", "--json").stdout)["build"]
              for name in ("controller", "target")}
    assert owners["controller"]["pid"] != owners["target"]["pid"]
    for owner in owners.values():
        assert owner["runtime_owner_protocol"] == 1 and owner["launch_id"] and owner["activation_id"]
    cluster.evidence["runtime_owners_before"] = owners
    cluster.evidence["authorization_mode"] = "shared grant; logical consent acceptance is separate"
    cluster.cli("target", "grant", "issue")
    grant = cluster.grant("target")
    cluster.cli("controller", "peer", "add", "--device", target, "--grant", grant)
    workspace = cluster.root / "workspaces" / "target"
    def git(*values):
        return subprocess.run(["git", "-C", str(workspace), *values], check=True, capture_output=True,
                              encoding="utf-8", errors="replace").stdout.strip()
    # Reuse an existing commit in an owned clone; never create a commit or
    # alter the source checkout merely to obtain a clean workspace fixture.
    source_revision = subprocess.run(["git", "-C", str(repo), "rev-parse", "HEAD"],
                                     check=True, capture_output=True, encoding="utf-8", errors="replace").stdout.strip()
    subprocess.run(["git", "clone", "--quiet", "--no-hardlinks", "--no-checkout",
                    "--single-branch", "--no-tags", str(repo), str(workspace)],
                   check=True, capture_output=True, encoding="utf-8", errors="replace", timeout=180)
    git("checkout", "--quiet", "--detach", source_revision)
    revision = git("rev-parse", "HEAD")
    assert revision == source_revision and git("status", "--porcelain") == ""
    cluster.evidence["workspace_fixture"] = {"mode": "clone-existing-commit", "revision": revision}
    fake = prepare_agent(cluster, args)
    cluster.cli("target", "handoff", "configure", "--executable", str(fake))
    base = dict(workspace=str(workspace), revision=revision, agent="cursor", session_id=str(uuid.uuid4()),
                mode="ask", prompt="Review the fixture.", trust_workspace=True, timeout_seconds=30,
                attachments=[dict(path="README.md", sha256=hashlib.sha256((workspace / "README.md").read_bytes()).hexdigest())])
    def submit(value, check=True):
        request = cluster.root / "request.json"
        request.write_text(json.dumps(value), encoding="utf-8")
        result = cluster.cli("controller", "handoff", "submit", "--device", target, "--request", str(request), check=check)
        return json.loads(result.stdout) if check else result
    def get(request_id):
        return json.loads(cluster.cli("controller", "handoff", "get", "--device", target, "--request-id", request_id).stdout)
    def wait(request_id, states):
        deadline = time.monotonic() + 40
        while True:
            value = get(request_id)
            if value["state"] in states:
                return value
            assert time.monotonic() < deadline, value
            time.sleep(.1)
    def count():
        return len((cluster.root / "target" / "fixture-agent-count").read_text().splitlines())
    request = dict(base, request_id="one")
    assert submit(request)["state"] == "received"
    result = wait("one", {"completed", "failed", "unknown"})
    assert result["state"] == "completed" and result["result"] == "handoff-fixture-ok", result
    assert result["session_id"] == base["session_id"] and result["request_id"] == "one", result
    assert Path(result["workspace"]).resolve() == workspace.resolve() and result["revision"] == revision, result
    assert count() == 1
    assert submit(request)["state"] == "completed" and count() == 1
    assert submit(dict(request, prompt="changed"), check=False).returncode != 0
    assert count() == 1
    passed("exact_session_receipt_and_duplicate_conflict_protection")

    for request_id, marker, state, code in [
        ("wrong-session", "TEST_WRONG_SESSION", "unknown", "session_mismatch"),
        ("missing-result", "TEST_MISSING_RESULT", "unknown", "missing_cli_result"),
        ("error-result", "TEST_ERROR_RESULT", "failed", "cli_result_error"),
        ("nonzero-exit", "TEST_NONZERO_EXIT", "unknown", "cli_exit_nonzero"),
        ("unconfirmed-large", "TEST_UNCONFIRMED_LARGE", "unknown", "unconfirmed_cli_result"),
        ("large-result", "TEST_LARGE_RESULT", "completed", None),
    ]:
        submit(dict(base, request_id=request_id, prompt=marker))
        result = wait(request_id, {"completed", "failed", "unknown"})
        assert result["state"] == state, result
        if code:
            assert result["code"] == code, result
            assert len(result.get("result", "").encode("utf-8")) <= 65536
        else:
            assert result["truncated"] and len(result["result"].encode("utf-8")) <= 65536
    passed("wrong_session_missing_terminal_and_bounded_unicode_result")

    for mode in ("plan", "agent"):
        submit(dict(base, request_id="mode-" + mode, mode=mode, prompt="TEST_MODE_" + mode.upper()))
        assert wait("mode-" + mode, {"completed", "failed", "unknown"})["state"] == "completed"
    assert submit(dict(base, request_id="no-mode", mode=""), check=False).returncode != 0
    passed("explicit_mode_forwarding_without_implicit_agent_mode")

    # The request budget includes five native preflight launches. Leave enough
    # time to observe the running child on loaded Windows hosts, then still
    # require its actual deadline (well before the fixture's 60-second sleep).
    timeout_seconds = 30
    timeout_started = time.monotonic()
    submit(dict(base, request_id="timeout", prompt="TEST_WAIT", timeout_seconds=timeout_seconds))
    assert wait("timeout", {"running"})["state"] == "running"
    timed_pid = int((cluster.root / "target" / "fixture-agent-pid").read_text())
    timed_ref = ProcessRef(timed_pid)
    references.append(timed_ref)
    assert timed_ref.alive(), "deadline fixture exited before observation"
    assert wait("timeout", {"unknown", "completed", "failed"})["code"] == "interrupted"
    assert time.monotonic() - timeout_started < timeout_seconds + 10, "deadline did not bound the real CLI"
    assert not timed_ref.alive(), "owned CLI survived timeout"
    passed("deadline_returns_unknown_and_reclaims_owned_cli")

    previous = count()
    submit(dict(base, request_id="wrong-revision", revision="0" * 40))
    result = wait("wrong-revision", {"failed"})
    assert result["code"] == "revision_mismatch" and count() == previous
    submit(dict(base, request_id="bad-attachment", attachments=[dict(path="../outside", sha256="0" * 64)]))
    assert wait("bad-attachment", {"failed"})["code"] == "attachment_outside_workspace"
    assert count() == previous
    (workspace / "README.md").write_text("dirty", encoding="utf-8")
    submit(dict(base, request_id="dirty-workspace"))
    assert wait("dirty-workspace", {"failed"})["code"] == "workspace_dirty"
    git("checkout", "--", "README.md")
    submit(dict(base, request_id="directory-attachment", attachments=[dict(path=".git", sha256="0" * 64)]))
    assert wait("directory-attachment", {"failed"})["code"] == "attachment_unavailable"
    assert count() == previous
    passed("repository_and_attachment_binding_precede_cli_launch")

    running = dict(base, request_id="restart", prompt="TEST_WAIT")
    submit(running)
    assert wait("restart", {"running"})["state"] == "running"
    assert submit(dict(running, request_id="concurrent"), check=False).returncode != 0
    previous = count()
    pid = int((cluster.root / "target" / "fixture-agent-pid").read_text())
    running_ref = ProcessRef(pid)
    references.append(running_ref)
    assert running_ref.alive(), "restart fixture exited before stop"
    cluster.stop("target")
    assert not running_ref.alive(), "owned CLI survived Runtime stop"
    cluster.cli("target", "ensure")
    restarted = json.loads(cluster.cli("target", "status", "--json").stdout)["build"]
    assert restarted["launch_id"] and restarted["launch_id"] != owners["target"]["launch_id"]
    assert restarted["sha256"] == owners["target"]["sha256"]
    cluster.evidence["target_owner_after_restart"] = restarted
    deadline = time.monotonic() + 15
    while True:
        ready = cluster.cli("controller", "doctor", "--device", target, "--json", check=False)
        if ready.returncode == 0:
            break
        assert time.monotonic() < deadline
        time.sleep(.1)
    recovered = get("restart")
    assert recovered["state"] == "unknown" and recovered["session_id"] == base["session_id"] and recovered["request_id"] == "restart"
    assert submit(running)["state"] == "unknown" and count() == previous
    passed("runtime_stop_restart_returns_unknown_without_replay")
finally:
    try:
        for reference in references:
            reference.close()
    finally:
        cluster.close()
output.write_text(json.dumps(completed_report(cluster,results),indent=2),encoding="utf-8")
