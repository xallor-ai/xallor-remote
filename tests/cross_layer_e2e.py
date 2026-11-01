"""Run real IPC/Relay executions, then verify reconnection and actual MCP calls."""
import argparse
import json
import os
from pathlib import Path
import subprocess
import sys
import time
from fixtures.runtime_cluster import Cluster

sys.stdout.reconfigure(encoding="utf-8", errors="replace")
ROOT = Path(__file__).resolve().parents[1]


def main():
    parser = argparse.ArgumentParser()
    platform = "windows-amd64/xallor-remote.exe" if os.name == "nt" else "linux-amd64/xallor-remote"
    parser.add_argument("--binary", default=str(ROOT / "dist/release" / platform))
    args = parser.parse_args()
    cluster = Cluster(args.binary)
    results = []
    try:
        cluster.start()
        target = cluster.add("target")
        cluster.add("controller")
        cluster.cli("target", "inbound", "on")
        grant = (cluster.root / "target/grant").read_text(encoding="utf-8")
        cluster.cli("controller", "peer", "add", "--id", target, "--grant", grant)
        bad_exit = cluster.cli("controller", "exec", "--device", target, "--", "exit 7", check=False)
        assert bad_exit.returncode != 0, "CLI falsely reported nonzero remote exit as success"
        results.append({"name":"cli_propagates_nonzero_exit", "status":"pass"})
        env = dict(cluster.env("controller"), XALLOR_TEST_CLUSTER=str(cluster.root),
                   XALLOR_TEST_TARGET=target, XALLOR_TEST_RELAY_PID=str(cluster.relay.pid))
        subprocess.run(["node", str(ROOT / "tests/helpers/cross-layer.mjs")], env=env, check=True, timeout=150)
        results += json.loads((cluster.root / "cross-layer-results.json").read_text(encoding="utf-8"))
        cluster.restart_relay()
        deadline = time.monotonic() + 45
        while True:
            checked = cluster.cli("controller", "doctor", "--device", target, "--json", check=False)
            if checked.returncode == 0:
                break
            assert time.monotonic() < deadline, "runtimes failed to reconnect after Relay restart"
            time.sleep(.2)
        executed = cluster.cli("controller", "exec", "--device", target, "--", "echo after-reconnect")
        assert "after-reconnect" in executed.stdout
        assert json.loads(cluster.cli("target", "status", "--json").stdout)["device_id"] == target
        results.append({"name":"relay_restart_reconnects_without_identity_change", "status":"pass"})
        subprocess.run(["node", str(ROOT / "tests/helpers/mcp-release-smoke.mjs")],
                       env=dict(cluster.env("controller"), XALLOR_TEST_CLI=str(cluster.binary), XALLOR_TEST_TARGET=target), check=True, timeout=60)
        results.append({"name":"actual_mcp_executes_and_recovers_after_runtime_restart", "status":"pass"})
    finally:
        cluster.close()
        output = ROOT / "dist/verification/cross-layer.json"
        output.parent.mkdir(parents=True, exist_ok=True)
        output.write_text(json.dumps(results, indent=2), encoding="utf-8")
    print(json.dumps(results), flush=True)


if __name__ == "__main__":
    main()
