"""Run real IPC/Relay executions, then verify reconnection and actual MCP calls."""
import argparse
from contextlib import closing
import hashlib
import json
import os
from pathlib import Path
import subprocess
import sqlite3
import sys
import time
from fixtures.runtime_cluster import Cluster

sys.stdout.reconfigure(encoding="utf-8", errors="replace")
ROOT = Path(__file__).resolve().parents[1]


def main():
    parser = argparse.ArgumentParser()
    platform = "windows-amd64/xallor-remote.exe" if os.name == "nt" else "linux-amd64/xallor-remote"
    parser.add_argument("--binary", default=str(ROOT / "dist/release" / platform))
    parser.add_argument("--go", default="go")
    args = parser.parse_args()
    cluster = Cluster(args.binary)
    results = []
    try:
        tree = cluster.root / ("tree.exe" if os.name == "nt" else "tree")
        subprocess.run([args.go, "build", "-o", str(tree), "./tests/fixtures/process-tree"], cwd=ROOT, check=True, timeout=180)
        cluster.start()
        target = cluster.add("target")
        cluster.add("controller")
        cluster.cli("target", "inbound", "on")
        grant = cluster.grant("target")
        cluster.cli("controller", "peer", "add", "--id", target, "--grant", grant)
        bad_exit = cluster.cli("controller", "exec", "--device", target, "--", "exit 7", check=False)
        assert bad_exit.returncode != 0, "CLI falsely reported nonzero remote exit as success"
        results.append({"name":"cli_propagates_nonzero_exit", "status":"pass"})
        env = dict(cluster.env("controller"), XALLOR_TEST_CLUSTER=str(cluster.root),
                   XALLOR_TEST_TARGET=target, XALLOR_TEST_RELAY_PID=str(cluster.relay.pid))
        subprocess.run(["node", str(ROOT / "tests/helpers/cross-layer.mjs")], env=env, check=True, timeout=210)
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
        # Inspect the real Relay database after exec, files, cancellation, and restart.
        # Operation payloads and output must not acquire a persistence collection.
        with closing(sqlite3.connect((cluster.root / "relay/relay.db").as_uri() + "?mode=ro", uri=True)) as db:
            assert db.execute("SELECT name FROM sqlite_master WHERE type='table' ORDER BY name").fetchall() == [(name,) for name in ("authority", "device_grants", "device_ownership", "device_shares", "devices", "memberships", "principal_credentials", "principals", "tenants")]
            assert db.execute("PRAGMA user_version").fetchone() == (5,)
            assert db.execute("SELECT inbound FROM devices WHERE id=?", (target,)).fetchone() == (1,)
            uid, grant_id, token_hash = db.execute("SELECT d.uid,g.id,g.token_hash FROM devices d JOIN device_grants g ON g.device_uid=d.uid WHERE d.id=?", (target,)).fetchone()
            assert len(uid) == 32 and len(grant_id) == 32 and uid != target
            current_grant = cluster.grant("target")
            assert token_hash == hashlib.sha256(current_grant.encode("utf-8")).hexdigest()
        results.append({"name":"relay_persists_identity_without_operation_payloads", "status":"pass"})
    finally:
        cluster.close()
        output = ROOT / "dist/verification/cross-layer.json"
        output.parent.mkdir(parents=True, exist_ok=True)
        output.write_text(json.dumps(results, indent=2), encoding="utf-8")
    print(json.dumps(results), flush=True)


if __name__ == "__main__":
    main()
