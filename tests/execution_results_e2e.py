"""Real TUI RPC and MCP stdio calls through owned Runtime/Relay processes."""
import argparse
import json
import os
from pathlib import Path
import subprocess
import sys
import time

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
try:
    fixture = cluster.root/("execution-result.exe" if os.name == "nt" else "execution-result")
    subprocess.run([args.go, "build", "-o", str(fixture), "./tests/fixtures/execution-result"], cwd=root, check=True, timeout=180)
    cluster.start()
    target = cluster.add("target")
    cluster.add("controller")
    cluster.cli("target", "inbound", "on")
    grant = cluster.grant("target")
    cluster.cli("controller", "peer", "add", "--device", target, "--grant", grant)
    subprocess.run([args.go, "test", "./internal/tui", "-run", "TestStreamExecE2E", "-count=1", "-timeout=60s"], cwd=root,
                   env=dict(cluster.env("controller"), XR_TUI_RESULT_E2E="1"), check=True, timeout=180)
    results.append("tui_real_stream_preserves_output_and_nonzero_exit")
    truncated = cluster.cli("controller", "exec", "--device", target, "--program", str(fixture), "--", "truncated", check=False)
    assert truncated.returncode != 0 and "退出码 7" in truncated.stderr and "截断" in truncated.stderr
    results.append("cli_reports_truncation_and_failure_together")
    env = dict(cluster.env("controller"), XALLOR_TEST_CLUSTER=str(cluster.root), XALLOR_TEST_TARGET=target,
               XALLOR_TEST_PROGRAM=str(fixture), XALLOR_TEST_RELAY_PID=str(cluster.relay.pid))
    subprocess.run(["node", str(root/"tests/helpers/execution-results.mjs")], env=env, check=True, timeout=150)
    results.append("mcp_preserves_failure_timeout_truncation_and_disconnect_evidence")
    cluster.relay.wait(timeout=10)
    cluster.restart_relay()
    deadline = time.monotonic()+30
    while cluster.cli("controller", "doctor", "--device", target, "--json", check=False).returncode:
        assert time.monotonic() < deadline, "owned Runtime did not reconnect"
        time.sleep(.1)
    running = json.loads(cluster.cli("controller", "runtime", "status").stdout)["running"]
    env["XALLOR_TEST_CONTROLLER_PID"] = str(running["pid"])
    subprocess.run(["node", str(root/"tests/helpers/execution-results.mjs")], env=env, check=True, timeout=60)
    results.append("mcp_preserves_output_when_controller_crashes_without_terminal")
finally:
    cluster.close()
output = root/"dist/verification/execution-results.json"
output.parent.mkdir(parents=True, exist_ok=True)
output.write_text(json.dumps(results, indent=2), encoding="utf-8")
print(json.dumps(results), flush=True)
