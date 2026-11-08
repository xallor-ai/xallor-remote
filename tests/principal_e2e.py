"""Owned Relay/Runtime/CLI/MCP checks for optional authenticated caller identity."""
import argparse
import json
import os
from pathlib import Path
import subprocess
import sys
from fixtures.runtime_cluster import Cluster

sys.stdout.reconfigure(encoding="utf-8", errors="replace")
ROOT = Path(__file__).resolve().parents[1]


def main():
    platform = "windows-amd64/xallor-remote.exe" if os.name == "nt" else "linux-amd64/xallor-remote"
    parser = argparse.ArgumentParser()
    parser.add_argument("--binary", default=str(ROOT / "dist/release" / platform))
    args = parser.parse_args()
    cluster = Cluster(args.binary)
    results = []
    try:
        cluster.start()
        target = cluster.add("target")
        cluster.add("controller")
        cluster.cli("target", "inbound", "on")
        grant = cluster.grant("target")
        cluster.cli("controller", "peer", "add", "--device", target, "--grant", grant)
        admin = ["relay", "--data", str(cluster.root / "relay"), "principal"]
        alice = json.loads(cluster.cli("controller", *admin, "create", "--name", "alice").stdout)
        bob = json.loads(cluster.cli("controller", *admin, "create", "--name", "bob").stdout)
        assert alice["principal_id"] != bob["principal_id"] and alice["tenant_id"] != bob["tenant_id"]
        for name, data in [("alice", alice), ("bob", bob)]:
            (cluster.root / f"{name}.json").write_text(json.dumps(data), encoding="utf-8")
        cluster.cli("controller", "account", "use", "--credential-file", str(cluster.root / "alice.json"))
        status = cluster.cli("controller", "account", "status").stdout
        assert json.loads(status) == {"configured": True} and alice["token"] not in status
        cluster.cli("controller", "doctor", "--device", target, "--json")
        assert "principal-e2e" in cluster.cli("controller", "exec", "--device", target, "--", "echo principal-e2e").stdout
        results.append({"name": "cli_import_authenticates_without_exposing_token", "status": "pass"})
        subprocess.run(["node", str(ROOT / "tests/helpers/mcp-release-smoke.mjs")],
                       env=dict(cluster.env("controller"), XALLOR_TEST_CLI=str(cluster.binary), XALLOR_TEST_TARGET=target),
                       check=True, timeout=90)
        results.append({"name": "mcp_uses_profile_identity_across_runtime_restart", "status": "pass"})
        subprocess.run(["node", str(ROOT / "tests/helpers/principal.mjs")],
                       env=dict(cluster.env("controller"), XALLOR_TEST_CLUSTER=str(cluster.root),
                                XALLOR_TEST_CLI=str(cluster.binary), XALLOR_TEST_TARGET=target), check=True, timeout=90)
        results += json.loads((cluster.root / "principal-results.json").read_text(encoding="utf-8"))
        for raw in ('{"token":', 'null', '{}', '{"relay":"","token":null}',
                    '{"relay":"","token":"","token":""}',
                    json.dumps({"relay": cluster.url, "token": ""})):
            cluster.stop("controller")
            (cluster.root / "controller/account.json").write_text(raw, encoding="utf-8")
            cluster.cli("controller", "ensure")
            assert json.loads(cluster.cli("controller", "account", "status").stdout)["configured"] is True
            denied = cluster.cli("controller", "exec", "--device", target, "--", "echo invalid-credential", check=False)
            assert denied.returncode != 0 and "invalid-credential" not in denied.stdout
            cluster.cli("controller", "account", "clear")
            assert "recovered-credential" in cluster.cli("controller", "exec", "--device", target, "--", "echo recovered-credential").stdout
        results.append({"name": "corrupt_profile_blocks_remote_but_preserves_local_recovery", "status": "pass"})
    finally:
        cluster.close()
        output = ROOT / "dist/verification/principal.json"
        output.parent.mkdir(parents=True, exist_ok=True)
        output.write_text(json.dumps(results, indent=2), encoding="utf-8")
    print(json.dumps(results), flush=True)


if __name__ == "__main__":
    main()
