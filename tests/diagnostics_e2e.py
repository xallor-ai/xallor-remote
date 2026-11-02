"""Exercise real CLI -> IPC -> Runtime -> local Relay -> target info diagnostics."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import sys
import time
from fixtures.runtime_cluster import Cluster

sys.stdout.reconfigure(encoding="utf-8", errors="replace")
ROOT = Path(__file__).resolve().parents[1]


def main():
    p = argparse.ArgumentParser()
    platform = "windows-amd64/xallor-remote.exe" if os.name == "nt" else "linux-amd64/xallor-remote"
    p.add_argument("--binary", default=str(ROOT / "dist/release" / platform))
    args = p.parse_args()
    cluster = Cluster(args.binary)
    results = []

    def diagnose(name, device=""):
        cmd = ["doctor", "--json"] + (["--device", device] if device else [])
        response = cluster.cli(name, *cmd, check=False)
        r = json.loads(response.stdout)
        assert (response.returncode == 0) == r["ok"]
        return r

    def expect(layer, state, code, device):
        deadline = time.monotonic() + 10
        while True:
            r = diagnose("controller", device)
            check = next(c for c in r["checks"] if c["layer"] == layer)
            if check["state"] == state and check.get("code", "") == code:
                results.append({"layer":layer, "state":state, "code":code, "status":"pass"})
                return r
            assert time.monotonic() < deadline, r
            time.sleep(.1)

    def snapshot():
        return {str(f.relative_to(cluster.root)):hashlib.sha256(f.read_bytes()).hexdigest()
                for name in cluster.profiles for f in (cluster.root / name).glob("*")
                if f.is_file() and f.name in ("identity.json", "device_secret", "grant", "peers.json", "config.json")}

    try:
        cluster.start()
        absent = diagnose("absent")
        assert not absent["ok"] and not (cluster.root / "absent").exists()
        results.append({"name":"missing_profile_is_not_created_or_started", "status":"pass"})
        target = cluster.add("target")
        cluster.add("controller")
        cluster.cli("target", "inbound", "on")
        grant = (cluster.root / "target/grant").read_text()
        expect("peer", "fail", "not_paired", target)
        cluster.cli("controller", "peer", "add", "--id", target, "--grant", "wrong-grant")
        expect("authorization", "fail", "unauthorized", target)
        cluster.cli("controller", "peer", "add", "--id", target, "--grant", grant)
        before = snapshot()
        good = expect("target_runtime", "ok", "", target)
        assert good["ok"] and before == snapshot()
        status = cluster.cli("controller", "status", "--json").stdout
        assert grant not in status and "wrong-grant" not in status
        cluster.cli("target", "inbound", "off")
        before = snapshot()
        expect("inbound", "fail", "inbound_disabled", target)
        assert before == snapshot()
        cluster.stop("target")
        expect("target_online", "fail", "device_offline", target)
        cluster.relay.terminate()
        cluster.relay.wait(timeout=10)
        expect("relay_transport", "fail", "connection_failed", target)
    finally:
        cluster.close()
    out = ROOT / "dist/verification/diagnostics.json"
    out.parent.mkdir(parents=True, exist_ok=True)
    out.write_text(json.dumps(results, indent=2), encoding="utf-8")
    print(json.dumps(results), flush=True)


if __name__ == "__main__":
    main()
