"""Real Runtime/CLI/MCP must not invoke through an unconfirmed client greeting."""
import argparse
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import time
import urllib.request
from fixtures.process_ref import ProcessRef

sys.stdout.reconfigure(encoding="utf-8", errors="replace")
ROOT = Path(__file__).resolve().parents[1]


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--binary", required=True)
    parser.add_argument("--go", default="go")
    args = parser.parse_args()
    binary = Path(args.binary).resolve()
    root = Path(tempfile.mkdtemp(prefix="xr-client-auth-")).resolve()
    profile = root / "profile"
    profile.mkdir()
    (root / ".owner").write_text("client-auth-e2e", encoding="utf-8")
    env = dict(os.environ, XALLOR_REMOTE_DATA_DIR=str(profile))
    for key in ("XALLOR_REMOTE_BIN", "XALLOR_REMOTE_DEVICE_ID", "XALLOR_REMOTE_DEVICE_GRANT"):
        env.pop(key, None)
    server = None
    runtime_ref = None
    results = []
    opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))

    def cli(*words, check=True):
        result = subprocess.run([str(binary), *words], env=env, capture_output=True,
                                encoding="utf-8", errors="replace", timeout=30)
        if check and result.returncode:
            raise AssertionError(f"CLI {words[0]} failed: {result.stderr}")
        return result

    try:
        fixture = root / ("unconfirmed-relay.exe" if os.name == "nt" else "unconfirmed-relay")
        subprocess.run([args.go, "build", "-o", str(fixture), "./tests/fixtures/unconfirmed-relay"], cwd=ROOT, check=True, timeout=180)
        ready = root / "ready"
        server = subprocess.Popen([str(fixture), "--ready", str(ready)], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        deadline = time.monotonic() + 15
        while not ready.exists():
            assert server.poll() is None and time.monotonic() < deadline, "fixture did not start"
            time.sleep(.05)
        url = ready.read_text(encoding="utf-8")
        env["XALLOR_REMOTE_RELAY_URL"] = url.replace("http://", "ws://") + "/remote"
        (profile / "config.json").write_text(json.dumps({"relay_url": env["XALLOR_REMOTE_RELAY_URL"], "workspace": str(root / "workspace")}), encoding="utf-8")
        cli("ensure")
        runtime_ref = ProcessRef(json.loads(cli("runtime", "status").stdout)["running"]["pid"])
        assert runtime_ref.alive()
        cli("peer", "add", "--id", "123456789", "--grant", "test-grant")
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
            subprocess.run(["node", str(ROOT / "tests/helpers/mcp-auth-confirmation.mjs")],
                           env=dict(env, XALLOR_REMOTE_BIN=str(binary)), check=True, timeout=45)
            with opener.open(url + "/stats", timeout=3) as response:
                stats = json.load(response)
            assert stats["invokes"] == 0 and stats["shared"] > 0
            if mode == "account":
                assert stats["accounts"] > 0
            results.append({"name": mode + "_cli_doctor_mcp_reject_unconfirmed_auth_without_invocation", "status": "pass"})
        print(json.dumps(results), flush=True)
    finally:
        try:
            cli("stop", check=False)
            if runtime_ref:
                deadline = time.monotonic() + 15
                while runtime_ref.alive():
                    assert time.monotonic() < deadline, "owned Runtime did not stop"
                    time.sleep(.05)
        finally:
            if runtime_ref:
                runtime_ref.close()
            if server and server.poll() is None:
                server.terminate()
                server.wait(timeout=10)
            output = ROOT / "dist/verification/client-auth.json"
            output.parent.mkdir(parents=True, exist_ok=True)
            output.write_text(json.dumps(results, indent=2), encoding="utf-8")


if __name__ == "__main__":
    main()
