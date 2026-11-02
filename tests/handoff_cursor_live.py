"""Opt-in authenticated Cursor Ask validation; never invoked by ordinary CI."""
import argparse
import json
from pathlib import Path
import subprocess
import sys
import tempfile
import time
import uuid
from fixtures.runtime_cluster import Cluster
from fixtures.cursor_session import create_chat

sys.stdout.reconfigure(encoding="utf-8", errors="replace")
parser = argparse.ArgumentParser()
parser.add_argument("--binary", required=True)
parser.add_argument("--executable", required=True)
parser.add_argument("--prefix-arg", action="append", default=[])
args = parser.parse_args()
agent = [str(Path(args.executable).resolve()), *args.prefix_arg]
cluster = Cluster(args.binary)
evidence = Path(tempfile.mkdtemp(prefix="xr-cursor-live-evidence-"))
print(f"Evidence: {evidence}", flush=True)
receipts = []
try:
    cluster.start()
    cluster.add("controller")
    target = cluster.add("target")
    cluster.cli("target", "grant", "issue")
    grant = (cluster.root / "target" / "grant").read_text(encoding="utf-8").strip()
    cluster.cli("controller", "peer", "add", "--id", target, "--grant", grant)
    workspace = cluster.root / "workspaces" / "target"
    def git(*values):
        return subprocess.run(["git", "-C", str(workspace), *values], check=True, capture_output=True,
                              encoding="utf-8", errors="replace", timeout=30).stdout.strip()
    git("init", "--quiet")
    (workspace / "REVIEW.md").write_text("This isolated repository contains no production code.\n", encoding="utf-8")
    git("add", "REVIEW.md")
    git("-c", "user.name=Xallor Test", "-c", "user.email=test@example.invalid", "commit", "--quiet", "-m", "fixture")
    revision = git("rev-parse", "HEAD")
    session, creation_exit = create_chat(agent, workspace)
    (evidence / "session-creation.json").write_text(json.dumps(dict(session_id=session,
        successful_exit_observed=creation_exit, allocator_stopped_after_id=not creation_exit)), encoding="utf-8")
    print(f"Allocated test session {session}; successful allocator exit: {creation_exit}", flush=True)
    configure = ["handoff", "configure", "--executable", agent[0]]
    for prefix in args.prefix_arg:
        configure += ["--prefix-arg", prefix]
    cluster.cli("target", *configure)
    base = dict(workspace=str(workspace), revision=revision, agent="cursor", session_id=session,
                mode="ask", trust_workspace=True, timeout_seconds=180)
    def submit(request):
        path = cluster.root / "request.json"
        path.write_text(json.dumps(request), encoding="utf-8")
        return json.loads(cluster.cli("controller", "handoff", "submit", "--device", target, "--request", str(path)).stdout)
    def wait(request_id):
        deadline = time.monotonic() + 195
        while time.monotonic() < deadline:
            receipt = json.loads(cluster.cli("controller", "handoff", "get", "--device", target,
                                             "--request-id", request_id).stdout)
            if receipt["state"] in ("completed", "failed", "unknown"):
                receipts.append(receipt)
                (evidence / "receipts.json").write_text(json.dumps(receipts, indent=2), encoding="utf-8")
                assert receipt["state"] == "completed", f"Cursor receipt: {receipt['state']} / {receipt.get('code')}"
                assert receipt["session_id"] == session and receipt["workspace"] == str(workspace)
                return receipt
            time.sleep(.5)
        raise AssertionError("Cursor receipt deadline exceeded")
    token = "XR_MEMORY_" + uuid.uuid4().hex
    first = dict(base, request_id="live-first", prompt=f"For this isolated Ask test, remember the token {token}. Reply with only that token. Do not edit any files or call external services.")
    submit(first)
    initial = wait("live-first")
    assert token in initial["result"], "first response did not confirm test token"
    print(f"PASS official CLI {initial['cli_version']} matching init/result in new session {session}", flush=True)
    second = dict(base, request_id="live-resume", prompt="What exact XR_MEMORY_ token did I provide in the preceding request in this session? Reply with only the token. Do not read files or edit anything.")
    submit(second)
    resumed = wait("live-resume")
    assert token in resumed["result"], "resumed session did not retain the previous request"
    assert submit(first) == initial, "duplicate submission changed its durable receipt"
    assert git("status", "--porcelain") == "", "Ask test modified the repository"
    (evidence / "verification.json").write_text(json.dumps(dict(passed=True, session_id=session,
        revision=revision, cli_version=initial["cli_version"], mode="ask", checks=["new-session", "matching-terminal",
        "resume-context", "duplicate-receipt", "workspace-unchanged"]), indent=2), encoding="utf-8")
    print("PASS same-session context, durable duplicate receipt, unchanged repository", flush=True)
finally:
    cluster.close()
