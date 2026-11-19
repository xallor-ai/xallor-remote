"""Real TUI RPC and MCP stdio calls through owned Runtime/Relay processes."""
import argparse
import json
import os
from pathlib import Path
import subprocess
import sys
import time

from fixtures.mechanics_cli import add_mechanics_options, cluster_from_options, completed_report

sys.stdout.reconfigure(encoding="utf-8", errors="replace")
root = Path(__file__).resolve().parents[1]
parser = argparse.ArgumentParser()
platform = "windows-amd64/xallor-remote.exe" if os.name == "nt" else "linux-amd64/xallor-remote"
parser.add_argument("--binary", default=str(root/"dist/release"/platform))
parser.add_argument("--go", default="go")
add_mechanics_options(parser, add_go=False)
args = parser.parse_args()
output = root/"dist/verification/execution-results.json"
cluster = cluster_from_options(args, output, __file__)
quiet = {"creationflags":subprocess.CREATE_NO_WINDOW} if os.name=="nt" else {}
results = []
try:
    fixture = cluster.root/("execution-result.exe" if os.name == "nt" else "execution-result")
    subprocess.run([args.go, "build", "-o", str(fixture), "./tests/fixtures/execution-result"], cwd=root, check=True, timeout=180, **quiet)
    cluster.start()
    target = cluster.add("target")
    cluster.add("controller")
    cluster.cli("target", "inbound", "on")
    grant = cluster.grant("target")
    cluster.cli("controller", "peer", "add", "--device", target, "--grant", grant)
    subprocess.run([args.go, "test", "./internal/tui", "-run", "TestStreamExecE2E", "-count=1", "-timeout=60s"], cwd=root,
                   env=dict(cluster.env("controller"), XR_TUI_RESULT_E2E="1"), check=True, timeout=180, **quiet)
    results.append(dict(name="tui_real_stream_preserves_output_and_nonzero_exit", status="pass"))
    truncated = cluster.cli("controller", "exec", "--device", target, "--program", str(fixture), "--", "truncated", check=False)
    assert truncated.returncode != 0 and "退出码 7" in truncated.stderr and "截断" in truncated.stderr
    results.append(dict(name="cli_reports_truncation_and_failure_together", status="pass"))
    env = dict(cluster.env("controller"), XALLOR_TEST_CLUSTER=str(cluster.root), XALLOR_TEST_TARGET=target,
               XALLOR_TEST_PROGRAM=str(fixture), XALLOR_TEST_RELAY_PID=str(cluster.relay.pid))
    subprocess.run(["node", str(root/"tests/helpers/execution-results.mjs")], env=env, check=True, timeout=150, **quiet)
    results.append(dict(name="mcp_preserves_failure_timeout_truncation_and_disconnect_evidence", status="pass"))
    cluster.relay.wait(timeout=10)
    cluster.restart_relay()
    deadline = time.monotonic()+30
    while cluster.cli("controller", "doctor", "--device", target, "--json", check=False).returncode:
        assert time.monotonic() < deadline, "owned Runtime did not reconnect"
        time.sleep(.1)
    running = json.loads(cluster.cli("controller", "runtime", "status").stdout)["running"]
    env["XALLOR_TEST_CONTROLLER_PID"] = str(running["pid"])
    subprocess.run(["node", str(root/"tests/helpers/execution-results.mjs")], env=env, check=True, timeout=60, **quiet)
    results.append(dict(name="mcp_preserves_output_when_controller_crashes_without_terminal", status="pass"))
finally:
    cluster.close()
output.write_text(json.dumps(completed_report(cluster, results), indent=2), encoding="utf-8")
print(json.dumps(results), flush=True)
