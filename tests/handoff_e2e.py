"""Handoff state/dedupe/recovery over a real Relay and two Runtimes."""
import argparse
import ctypes
import hashlib
import json
import os
from pathlib import Path
import subprocess
import sys
import time
import uuid
from fixtures.runtime_cluster import Cluster

sys.stdout.reconfigure(encoding="utf-8", errors="replace")
parser = argparse.ArgumentParser()
parser.add_argument("--binary", required=True)
parser.add_argument("--go", default="go")
args = parser.parse_args()
repo = Path(__file__).resolve().parents[1]
cluster = Cluster(args.binary)
try:
    cluster.start()
    controller, target = cluster.add("controller"), cluster.add("target")
    cluster.cli("target", "grant", "issue")
    grant = cluster.grant("target")
    cluster.cli("controller", "peer", "add", "--id", target, "--grant", grant)
    workspace = cluster.root / "workspaces" / "target"
    def git(*values):
        return subprocess.run(["git", "-C", str(workspace), *values], check=True, capture_output=True,
                              encoding="utf-8", errors="replace").stdout.strip()
    git("init", "--quiet")
    (workspace / "review.md").write_text("A harmless review fixture.\n", encoding="utf-8")
    git("add", "review.md")
    git("-c", "user.name=Xallor Test", "-c", "user.email=test@example.invalid", "commit", "--quiet", "-m", "fixture")
    revision = git("rev-parse", "HEAD")
    fake = cluster.root / ("fixture-cursor.exe" if os.name == "nt" else "fixture-cursor")
    subprocess.run([args.go, "build", "-o", str(fake), "./tests/fixtures/cursor-cli"], cwd=repo, check=True)
    cluster.cli("target", "handoff", "configure", "--executable", str(fake))
    base = dict(workspace=str(workspace), revision=revision, agent="cursor", session_id=str(uuid.uuid4()),
                mode="ask", prompt="Review the fixture.", trust_workspace=True, timeout_seconds=30,
                attachments=[dict(path="review.md", sha256=hashlib.sha256((workspace / "review.md").read_bytes()).hexdigest())])
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
    def process_alive(pid):
        if os.name == "nt":
            kernel = ctypes.WinDLL("kernel32", use_last_error=True)
            kernel.OpenProcess.restype = ctypes.c_void_p
            kernel.OpenProcess.argtypes = [ctypes.c_uint32, ctypes.c_int, ctypes.c_uint32]
            kernel.WaitForSingleObject.argtypes = [ctypes.c_void_p, ctypes.c_uint32]
            kernel.CloseHandle.argtypes = [ctypes.c_void_p]
            handle = kernel.OpenProcess(0x100000, 0, pid)
            if not handle:
                assert ctypes.get_last_error() == 87, "cannot inspect fixture process"
                return False
            try:
                return kernel.WaitForSingleObject(handle, 0) == 258
            finally:
                kernel.CloseHandle(handle)
        try:
            os.kill(pid, 0)
            return True
        except ProcessLookupError:
            return False
    request = dict(base, request_id="one")
    assert submit(request)["state"] == "received"
    result = wait("one", {"completed", "failed", "unknown"})
    assert result["state"] == "completed" and result["result"] == "handoff-fixture-ok", result
    assert count() == 1
    assert submit(request)["state"] == "completed" and count() == 1
    assert submit(dict(request, prompt="changed"), check=False).returncode != 0
    assert count() == 1
    print("PASS exact session receipt and duplicate/conflict protection")

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
    print("PASS wrong session, missing terminal, bounded Unicode result")

    for mode in ("plan", "agent"):
        submit(dict(base, request_id="mode-" + mode, mode=mode, prompt="TEST_MODE_" + mode.upper()))
        assert wait("mode-" + mode, {"completed", "failed", "unknown"})["state"] == "completed"
    assert submit(dict(base, request_id="no-mode", mode=""), check=False).returncode != 0
    print("PASS explicit mode forwarding; no implicit agent mode")

    # The request budget includes five native preflight launches. Leave enough
    # time to observe the running child on loaded Windows hosts, then still
    # require its actual deadline (well before the fixture's 60-second sleep).
    timeout_seconds = 30
    timeout_started = time.monotonic()
    submit(dict(base, request_id="timeout", prompt="TEST_WAIT", timeout_seconds=timeout_seconds))
    assert wait("timeout", {"running"})["state"] == "running"
    timed_pid = int((cluster.root / "target" / "fixture-agent-pid").read_text())
    assert wait("timeout", {"unknown", "completed", "failed"})["code"] == "interrupted"
    assert time.monotonic() - timeout_started < timeout_seconds + 10, "deadline did not bound the real CLI"
    assert not process_alive(timed_pid), "owned CLI survived timeout"
    print("PASS deadline returns unknown and reclaims owned CLI")

    previous = count()
    submit(dict(base, request_id="wrong-revision", revision="0" * 40))
    result = wait("wrong-revision", {"failed"})
    assert result["code"] == "revision_mismatch" and count() == previous
    submit(dict(base, request_id="bad-attachment", attachments=[dict(path="../outside", sha256="0" * 64)]))
    assert wait("bad-attachment", {"failed"})["code"] == "attachment_outside_workspace"
    assert count() == previous
    (workspace / "review.md").write_text("dirty", encoding="utf-8")
    submit(dict(base, request_id="dirty-workspace"))
    assert wait("dirty-workspace", {"failed"})["code"] == "workspace_dirty"
    git("checkout", "--", "review.md")
    submit(dict(base, request_id="directory-attachment", attachments=[dict(path=".git", sha256="0" * 64)]))
    assert wait("directory-attachment", {"failed"})["code"] == "attachment_unavailable"
    assert count() == previous
    print("PASS repository/attachment binding before CLI launch")

    running = dict(base, request_id="restart", prompt="TEST_WAIT")
    submit(running)
    assert wait("restart", {"running"})["state"] == "running"
    assert submit(dict(running, request_id="concurrent"), check=False).returncode != 0
    previous = count()
    pid = int((cluster.root / "target" / "fixture-agent-pid").read_text())
    cluster.stop("target")
    assert not process_alive(pid), "owned CLI survived Runtime stop"
    cluster.cli("target", "ensure")
    deadline = time.monotonic() + 15
    while True:
        ready = cluster.cli("controller", "doctor", "--device", target, "--json", check=False)
        if ready.returncode == 0:
            break
        assert time.monotonic() < deadline
        time.sleep(.1)
    assert get("restart")["state"] == "unknown"
    assert submit(running)["state"] == "unknown" and count() == previous
    print("PASS Runtime stop/restart returns unknown without replay")
finally:
    cluster.close()
