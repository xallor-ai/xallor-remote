"""Real device-side ownership management and scoped client access on owned profiles."""
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
        alice = json.loads(cluster.cli("target", *admin, "create", "--name", "alice").stdout)
        bob = json.loads(cluster.cli("target", *admin, "create", "--name", "bob").stdout)
        colleague = json.loads(cluster.cli("target", *admin, "create", "--name", "colleague", "--tenant", alice["tenant_id"]).stdout)
        for name, data in [("alice", alice), ("bob", bob), ("colleague", colleague)]:
            (cluster.root / f"{name}.json").write_text(json.dumps(data), encoding="utf-8")
        cluster.cli("target", "account", "use", "--credential-file", str(cluster.root / "alice.json"))
        cluster.cli("controller", "account", "use", "--credential-file", str(cluster.root / "bob.json"))
        subprocess.run(["node", str(ROOT / "tests/helpers/ownership.mjs")],
                       env=dict(cluster.env("controller"), XALLOR_TEST_CLUSTER=str(cluster.root),
                                XALLOR_TEST_CLI=str(cluster.binary), XALLOR_TEST_TARGET=target), check=True, timeout=180)
        results += json.loads((cluster.root / "ownership-results.json").read_text(encoding="utf-8"))
        before = json.loads(cluster.cli("target", "device", "owner").stdout)
        cluster.stop("target")
        cluster.cli("target", "ensure")
        deadline = time.monotonic() + 45
        while cluster.cli("controller", "doctor", "--device", target, "--json", check=False).returncode:
            assert time.monotonic() < deadline, "claimed device failed to reconnect"
            time.sleep(.1)
        after = json.loads(cluster.cli("target", "device", "owner").stdout)
        assert after == before
        results.append({"name": "reconnect_keeps_ownership_despite_local_bearer_sync", "status": "pass"})
        subprocess.run(["node", str(ROOT / "tests/helpers/mcp-release-smoke.mjs")],
                       env=dict(cluster.env("controller"), XALLOR_TEST_CLI=str(cluster.binary), XALLOR_TEST_TARGET=target),
                       check=True, timeout=90)
        results.append({"name": "actual_mcp_uses_owner_access_without_device_bearer", "status": "pass"})
    finally:
        cluster.close()
        output = ROOT / "dist/verification/ownership.json"
        output.parent.mkdir(parents=True, exist_ok=True)
        output.write_text(json.dumps(results, indent=2), encoding="utf-8")
    print(json.dumps(results), flush=True)


if __name__ == "__main__":
    main()
