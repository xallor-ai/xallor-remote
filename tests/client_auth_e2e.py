"""Real Runtime/CLI/MCP must not invoke through an unconfirmed client greeting."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import subprocess
import shutil
import sys
import urllib.request
from fixtures.process_ref import ProcessRef
from fixtures.ready_url import wait_ready_url
from fixtures.mechanics_cli import add_mechanics_options, cluster_from_options, completed_report

sys.stdout.reconfigure(encoding="utf-8", errors="replace")
ROOT = Path(__file__).resolve().parents[1]


def main():
    parser = argparse.ArgumentParser()
    platform = "windows-amd64/xallor-remote.exe" if os.name == "nt" else "linux-amd64/xallor-remote"
    parser.add_argument("--binary", default=str(ROOT / "dist/release" / platform))
    parser.add_argument("--go", default="go")
    add_mechanics_options(parser, add_go=False)
    parser.add_argument("--relay-fixture", help="Explicit trusted precompiled unconfirmed Relay test program")
    args = parser.parse_args()
    output = ROOT / "dist/verification/client-auth.json"
    cluster = cluster_from_options(args, output, __file__)
    binary, root = cluster.binary, cluster.root
    cluster.evidence["binary_sha256"] = hashlib.sha256(binary.read_bytes()).hexdigest()
    profile = root / "profile"
    profile.mkdir()
    cluster.profiles.append("profile")
    quiet = {"creationflags":subprocess.CREATE_NO_WINDOW} if os.name == "nt" else {}
    runtime_ref = None
    results = []
    opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))

    def cli(*words, check=True):
        return cluster.cli("profile", *words, check=check)

    cluster.url = "ws://127.0.0.1:1/remote"
    try:
        fixture = root / ("unconfirmed-relay.exe" if os.name == "nt" else "unconfirmed-relay")
        if args.relay_fixture:
            shutil.copy2(Path(args.relay_fixture).resolve(), fixture)
            fixture.chmod(0o700)
        else:
            subprocess.run([args.go, "build", "-o", str(fixture), "./tests/fixtures/unconfirmed-relay"], cwd=ROOT, check=True, timeout=180, **quiet)
        cluster.evidence["unconfirmed_relay_sha256"] = hashlib.sha256(fixture.read_bytes()).hexdigest()
        ready = root / "ready"
        cluster.relay = subprocess.Popen([str(fixture), "--ready", str(ready)], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL,
                                         env=cluster.base_env(), **quiet)
        url = wait_ready_url(ready, cluster.relay)
        cluster.url = url.replace("http://", "ws://") + "/remote"
        (profile / "config.json").write_text(json.dumps({"relay_url": cluster.url, "workspace": str(root / "workspace")}), encoding="utf-8")
        cli("ensure")
        runtime_ref = ProcessRef(json.loads(cli("runtime", "status").stdout)["running"]["pid"])
        assert runtime_ref.alive()
        cli("peer", "add", "--device", "123456789", "--grant", "test-grant")
        for mode in ("shared", "account"):
            if mode == "account":
                credential = root / "account.json"
                credential.write_text(json.dumps({"token": "xr_account_" + "a" * 64}), encoding="utf-8")
                cli("account", "use", "--credential-file", str(credential))
            result = cli("exec", "--device", "123456789", "--jsonl", "--", "echo never-dispatch", check=False)
            rows = [json.loads(line) for line in result.stdout.splitlines()]
            assert result.returncode != 0 and len(rows) == 1 and rows[0]["type"] == "error" and rows[0]["code"] == "relay_error", rows
            report = json.loads(cli("doctor", "--device", "123456789", "--json", check=False).stdout)
            auth = next(row for row in report["checks"] if row["layer"] == "authorization")
            assert not report["ok"] and auth["state"] == "unknown" and auth["code"] == "invalid_response", report
            mcp = subprocess.run(["node", str(ROOT / "tests/helpers/mcp-auth-confirmation.mjs")],
                           env=dict(cluster.env("profile"), XALLOR_REMOTE_BIN=str(binary)), timeout=45,
                           capture_output=True, text=True, encoding="utf-8", errors="replace", **quiet)
            assert mcp.returncode == 0, (mcp.stdout, mcp.stderr)
            with opener.open(url + "/stats", timeout=3) as response:
                stats = json.load(response)
            assert stats["invokes"] == 0 and stats["shared"] > 0
            if mode == "account":
                assert stats["accounts"] > 0
            results.append({"name": mode + "_cli_doctor_mcp_reject_unconfirmed_auth_without_invocation", "status": "pass"})
    finally:
        try:
            cluster.close()
            if runtime_ref:
                assert not runtime_ref.alive(), "strict stop returned before the retained Runtime process exited"
        finally:
            if runtime_ref:
                runtime_ref.close()
    output.write_text(json.dumps(completed_report(cluster, results), indent=2), encoding="utf-8")
    print(json.dumps(results), flush=True)


if __name__ == "__main__":
    main()
