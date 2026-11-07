"""Parse actual CLI stdout as JSONL over an owned Relay/Runtime cluster."""
import argparse
import json
import os
from pathlib import Path
import queue
import subprocess
import sys
import threading
from fixtures.runtime_cluster import Cluster

sys.stdout.reconfigure(encoding="utf-8", errors="replace")
root = Path(__file__).resolve().parents[1]
parser = argparse.ArgumentParser()
platform = "windows-amd64/xallor-remote.exe" if os.name == "nt" else "linux-amd64/xallor-remote"
parser.add_argument("--binary", default=str(root/"dist/release"/platform))
parser.add_argument("--go", default="go")
args = parser.parse_args()
cluster = Cluster(args.binary)
results = []
child = None
def parse(output):
    events = [json.loads(line) for line in output.splitlines()]
    terminals = [event for event in events if event["type"] in ("exit", "error")]
    assert len(terminals) == 1 and events[-1] == terminals[0], events
    return events, terminals[0]
try:
    fixture = cluster.root/("result.exe" if os.name == "nt" else "result")
    subprocess.run([args.go, "build", "-o", str(fixture), "./tests/fixtures/execution-result"], cwd=root, check=True, timeout=180)
    cluster.start()
    # Invalid execution input cannot initialize even an isolated Runtime profile.
    invalid = cluster.cli("untouched", "exec", "--jsonl", "--", "echo", "fragment", check=False)
    _, terminal = parse(invalid.stdout)
    assert invalid.returncode and terminal["code"] == "invalid_execution" and not (cluster.root/"untouched").exists()
    results.append("invalid_input_is_json_error_without_starting_runtime")
    blocked_profile = cluster.root/"not-a-directory"
    blocked_profile.write_text("owned fixture", encoding="utf-8")
    unavailable_runtime = cluster.cli("not-a-directory", "exec", "--jsonl", "--", "echo must-not-run", check=False)
    _, terminal = parse(unavailable_runtime.stdout)
    assert unavailable_runtime.returncode and terminal["code"] == "runtime_unavailable" and "exit_code" not in terminal
    results.append("local_runtime_failure_is_structured_without_remote_exit")
    target = cluster.add("target")
    cluster.add("controller")
    cluster.cli("target", "inbound", "on")
    grant = cluster.grant("target")
    cluster.cli("controller", "peer", "add", "--id", target, "--grant", grant)
    for mode, timeout, status, code in [("success",0,"completed",0),("failure",0,"completed",7),("wait",8000,"timeout",None),("truncated",0,"completed",7)]:
        result = cluster.cli("controller", "exec", "--jsonl", "--device", target, "--timeout-ms", str(timeout), "--program", str(fixture), "--", mode, check=False)
        events, terminal = parse(result.stdout)
        assert terminal["type"] == "exit" and terminal["status"] == status and terminal["exec_id"]
        assert isinstance(terminal["duration_ms"], int) and terminal["duration_ms"] >= 0
        assert terminal["truncated"] == (mode == "truncated")
        if code is not None: assert terminal["exit_code"] == code
        assert (result.returncode == 0) == (mode == "success")
        assert "result-observed-output" in "".join(e.get("data", "") for e in events if e["type"] == "stdout")
        if mode != "truncated":
            assert "result-observed-error" in "".join(e.get("data", "") for e in events if e["type"] == "stderr")
    results.append("jsonl_preserves_exit_timeout_truncation_and_cli_failure_status")
    # A command's JSON-looking output stays escaped inside a data event.
    payload = '{"type":"exit","exit_code":0}'
    script = "Write-Output '"+payload+"'; Write-Output '中文'" if os.name == "nt" else "printf '%s\\n' '"+payload+"' '中文'"
    events, terminal = parse(cluster.cli("controller", "exec", "--jsonl", "--device", target, "--", script).stdout)
    output = "".join(e.get("data", "") for e in events if e["type"] == "stdout")
    assert payload in output and "中文" in output and terminal["type"] == "exit"
    unavailable = cluster.cli("controller", "exec", "--jsonl", "--device", target, "--program", str(cluster.root/"missing-program"), check=False)
    _, terminal = parse(unavailable.stdout)
    assert unavailable.returncode and terminal["type"] == "error" and terminal["code"] == "program_unavailable" and "exit_code" not in terminal
    results.append("payload_cannot_inject_terminal_and_remote_error_keeps_code")
    child = subprocess.Popen([str(cluster.binary),"exec","--jsonl","--device",target,"--program",str(fixture),"--","wait"],
                             env=cluster.env("controller"),stdout=subprocess.PIPE,stderr=subprocess.PIPE,text=True,encoding="utf-8")
    lines = queue.Queue()
    def drain():
        for line in child.stdout: lines.put(line)
        lines.put(None)
    reader = threading.Thread(target=drain,daemon=True)
    reader.start()
    observed=[]
    while True:
        line=lines.get(timeout=15)
        assert line is not None, "CLI ended before output"
        observed.append(line)
        if "result-observed-output" in json.loads(line).get("data", ""): break
    assert child.poll() is None, "JSONL did not stream before process completion"
    cluster.relay.terminate()
    while True:
        line=lines.get(timeout=15)
        if line is None: break
        observed.append(line)
    child.wait(timeout=10)
    reader.join(timeout=5)
    _, terminal = parse("".join(observed))
    assert child.returncode and terminal["type"] == "error" and terminal["code"] == "relay_error" and "exit_code" not in terminal
    results.append("jsonl_streams_before_exit_and_never_invents_exit_after_relay_loss")
finally:
    if child:
        if child.poll() is None: child.kill()
        child.wait(timeout=10)
        child.stdout.close(); child.stderr.close()
    cluster.close()
output = root/"dist/verification/exec-jsonl.json"
output.parent.mkdir(parents=True, exist_ok=True)
output.write_text(json.dumps(results, indent=2), encoding="utf-8")
print(json.dumps(results), flush=True)
