"""Exercise a real MCP call while retaining references to its owned process tree."""
import json
import queue
import subprocess
import threading
import time
from pathlib import Path

from .process_ref import ProcessRef


def verify_revocation(cluster, target, executable, revoke):
    directory = cluster.root / "workspaces/target/mcp-revocation"
    directory.mkdir()
    root = Path(__file__).resolve().parents[2]
    messages, refs = queue.Queue(), []
    stderr = (cluster.root / "mcp-stderr.txt").open("w", encoding="utf-8")
    child = subprocess.Popen(["node", str(root / "packages/mcp/dist/index.js")],
                             env=dict(cluster.env("controller"), XALLOR_REMOTE_BIN=str(cluster.binary)),
                             stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=stderr,
                             encoding="utf-8", errors="replace", bufsize=1)

    def collect():
        try:
            for line in child.stdout:
                messages.put(json.loads(line))
        finally:
            messages.put(None)

    reader = threading.Thread(target=collect, daemon=True)
    reader.start()

    def send(message):
        child.stdin.write(json.dumps(dict(jsonrpc="2.0", **message)) + "\n")
        child.stdin.flush()

    def receive():
        message = messages.get(timeout=35)
        assert message is not None and "error" not in message, message
        return message

    try:
        send(dict(id=1, method="initialize", params=dict(protocolVersion="2025-11-25", capabilities={}, clientInfo=dict(name="authorization-e2e", version="1"))))
        while receive().get("id") != 1:
            pass
        send(dict(method="notifications/initialized"))
        send(dict(id=2, method="tools/call", params=dict(name="remote.exec", arguments=dict(device_id=target,
                  program=dict(executable=str(executable), args=["parent", str(directory)])), _meta=dict(progressToken="started"))))
        while True:
            message = receive()
            assert message.get("id") != 2, "command ended before revocation"
            if message.get("method") == "notifications/progress" and "tree-started" in message.get("params", {}).get("message", ""):
                break
        pids = json.loads((directory / "pids.json").read_text(encoding="utf-8"))
        refs = [ProcessRef(pids[key]) for key in ("parent", "child")]
        assert all(ref.alive() for ref in refs)
        revoke()
        while True:
            message = receive()
            if message.get("id") == 2:
                break
        result = message["result"]
        text = "\n".join(item.get("text", "") for item in result["content"])
        assert result["isError"] is True, result
        for expected in ("tree-started", "status=authorization_lost", "授权已失效，任务已停止。", "exec_id="):
            assert expected in text, text
        assert "执行结果未确认" not in text and "exit_code=unknown" not in text, text
        deadline = time.monotonic() + 10
        while any(ref.alive() for ref in refs):
            assert time.monotonic() < deadline, "MCP returned before process cleanup"
            time.sleep(.05)
        assert not json.loads((cluster.root / "target/jobs.json").read_text(encoding="utf-8"))["jobs"]
    finally:
        child.stdin.close()
        if child.poll() is None:
            child.terminate()
        child.wait(timeout=10)
        reader.join(timeout=5)
        child.stdout.close()
        stderr.close()
        for ref in refs:
            ref.close()
