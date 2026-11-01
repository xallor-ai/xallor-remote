"""Real-process release install/upgrade/rollback E2E. Never uses the default profile."""
import argparse
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import shutil
import socket
import subprocess
import sys
import tempfile
import time
import urllib.request

sys.stdout.reconfigure(encoding="utf-8", errors="replace")
ROOT = Path(__file__).resolve().parents[1]
spec = importlib.util.spec_from_file_location("release_builder", ROOT / "scripts/build-release.py")
builder = importlib.util.module_from_spec(spec)
spec.loader.exec_module(builder)


def main():
    p = argparse.ArgumentParser()
    p.add_argument("--go", default="go")
    p.add_argument("--allow-dirty", action="store_true")
    args = p.parse_args()
    platform = "windows/amd64" if os.name == "nt" else "linux/amd64"
    root = Path(tempfile.mkdtemp(prefix="xr-release-")).resolve()
    assert root.parent == Path(tempfile.gettempdir()).resolve()
    (root / ".owner").write_text("release-e2e", encoding="utf-8")
    results = []
    profiles = [root / "target", root / "controller"]
    relay = None
    relay_log = None
    binary = None
    cleanup_ok = True

    def record(name, **details):
        results.append(dict(name=name, status="pass", **details))
        print(json.dumps(results[-1]), flush=True)

    def env(profile):
        return dict(os.environ, XALLOR_REMOTE_DATA_DIR=str(profile), XALLOR_REMOTE_RELAY_URL=relay_url)

    def cli(profile, *words, success=True, executable=None):
        r = subprocess.run([str(executable or binary), *words], env=env(profile),
                           capture_output=True, encoding="utf-8", errors="replace", timeout=40)
        if success and r.returncode:
            raise AssertionError(f"CLI {words[0]} failed: {r.stderr.strip()}")
        return r

    def state(profile):
        return json.loads(cli(profile, "runtime", "status").stdout)

    def snapshot(profile):
        return {name: hashlib.sha256((profile / name).read_bytes()).hexdigest()
                for name in ("identity.json", "device_secret", "grant", "peers.json", "config.json")}

    try:
        os.environ["SOURCE_DATE_EPOCH"] = "1793536800"
        a = builder.build(root / "a", platform, args.go, args.allow_dirty)
        os.environ["SOURCE_DATE_EPOCH"] = "1793536860"
        b = builder.build(root / "b", platform, args.go, args.allow_dirty)
        ma = json.loads((a / "manifest.json").read_text())
        mb = json.loads((b / "manifest.json").read_text())
        binary = a / ma["artifact"]["filename"]
        assert ma["artifact"]["sha256"] != mb["artifact"]["sha256"]
        with socket.socket() as s:
            s.bind(("127.0.0.1", 0))
            port = s.getsockname()[1]
        relay_url = f"ws://127.0.0.1:{port}/remote"
        relay_log = (root / "relay.log").open("wb")
        relay = subprocess.Popen([str(binary), "relay", "--listen", f"127.0.0.1:{port}",
                                  "--data", str(root / "relay")], stdout=relay_log, stderr=relay_log)
        deadline = time.monotonic() + 10
        while True:
            try:
                with urllib.request.urlopen(f"http://127.0.0.1:{port}/health", timeout=1) as response:
                    health = json.load(response)
                assert health["build"]["revision"] == ma["build"]["revision"]
                break
            except Exception:
                if time.monotonic() > deadline:
                    raise
                time.sleep(.1)
        for profile in profiles:
            profile.mkdir(parents=True)
            custom_workspace = root / "workspaces" / profile.name
            custom_workspace.mkdir(parents=True)
            (profile / "config.json").write_text(json.dumps({
                "relay_url": relay_url, "workspace": str(custom_workspace), "shell": ""
            }), encoding="utf-8")
            cli(profile, "runtime", "install", "--bundle", str(a))
        target, controller = profiles
        identity = json.loads((target / "identity.json").read_text())
        cli(target, "grant", "issue")
        grant = (target / "grant").read_text()
        cli(target, "peer", "add", "--id", "901234569", "--grant", "xr_grant_test_only")
        cli(controller, "peer", "add", "--id", identity["device_id"], "--grant", grant)
        cli(target, "inbound", "off")
        before = snapshot(target)
        old_pid = state(target)["running"]["pid"]
        cli(target, "ensure")
        assert state(target)["running"]["pid"] == old_pid
        record("first_install_and_repeat_ensure", pid=old_pid)

        cli(target, "runtime", "install", "--bundle", str(b))
        upgraded = state(target)
        assert upgraded["running"]["sha256"] == mb["artifact"]["sha256"]
        assert upgraded["running"]["pid"] != old_pid
        assert snapshot(target) == before
        record("upgrade_preserves_identity_grant_peers_workspace_and_inbound")

        time.sleep(.5)
        denied = cli(controller, "exec", "--device", identity["device_id"], "--", "echo release-e2e", success=False)
        assert denied.returncode != 0 and "开放入站" in denied.stderr, denied.stderr
        record("upgrade_reconnect_keeps_inbound_disabled")
        cli(target, "inbound", "on")
        cli(controller, "exec", "--device", identity["device_id"], "--", "echo release-e2e")
        cli(target, "inbound", "off")
        record("upgraded_runtime_executes_over_real_relay")

        cli(target, "runtime", "rollback")
        assert state(target)["running"]["sha256"] == ma["artifact"]["sha256"]
        assert snapshot(target) == before
        record("rollback_restores_verified_artifact_and_preserves_state")

        corrupt = root / "corrupt"
        shutil.copytree(b, corrupt)
        with (corrupt / mb["artifact"]["filename"]).open("ab") as f:
            f.write(b"tampered")
        pid = state(target)["running"]["pid"]
        rejected = cli(target, "runtime", "install", "--bundle", str(corrupt), success=False)
        assert rejected.returncode != 0 and state(target)["running"]["pid"] == pid
        assert snapshot(target) == before
        record("corrupt_bundle_rejected_without_stopping_runtime")

        # Check the recovery path with a real executable, not a mocked Start callback.
        failing = root / "failing"
        failing.mkdir()
        bad_binary = failing / ma["artifact"]["filename"]
        prefix = "github.com/xallor-ai/xallor-remote/internal/buildinfo."
        flags = (f"-X {prefix}Revision={ma['build']['revision']} "
                 f"-X {prefix}BuiltAt={ma['build']['built_at']} "
                 f"-X {prefix}Modified={str(ma['build']['dirty']).lower()}")
        subprocess.run([args.go, "build", "-buildvcs=false", "-ldflags", flags, "-o", str(bad_binary),
                        "./tests/fixtures/failing-runtime"], cwd=ROOT, check=True)
        bad_manifest = json.loads(json.dumps(ma))
        bad_manifest["artifact"]["sha256"] = hashlib.sha256(bad_binary.read_bytes()).hexdigest()
        bad_manifest["artifact"]["size"] = bad_binary.stat().st_size
        (failing / "manifest.json").write_text(json.dumps(bad_manifest), encoding="utf-8")
        rejected = cli(target, "runtime", "install", "--bundle", str(failing), success=False)
        assert rejected.returncode != 0
        recovered = state(target)
        assert recovered["running"]["sha256"] == ma["artifact"]["sha256"]
        assert "pending" not in recovered["installed"] and snapshot(target) == before
        record("failed_candidate_start_restores_previous_real_process")

        cli(target, "stop")
        deadline = time.monotonic() + 10
        while state(target)["running"] is not None:
            assert time.monotonic() < deadline
            time.sleep(.05)
        cli(target, "ensure", executable=b / mb["artifact"]["filename"])
        assert state(target)["running"]["sha256"] == ma["artifact"]["sha256"]
        assert snapshot(target) == before
        record("ensure_launches_selected_artifact_not_invoking_binary")

        # A second start must not replace the same profile's identity, socket or device connection.
        pid = state(target)["running"]["pid"]
        duplicate = cli(target, "start", success=False)
        assert duplicate.returncode != 0 and state(target)["running"]["pid"] == pid
        record("duplicate_start_rejected_without_harming_live_instance")
        smoke = subprocess.run(["node", str(ROOT / "tests/helpers/mcp-release-smoke.mjs")],
                               env=dict(env(target), XALLOR_TEST_CLI=str(binary)), capture_output=True, text=True, encoding="utf-8", timeout=60)
        assert smoke.returncode == 0, smoke.stderr
        record("real_mcp_connects_and_reconnects_after_runtime_restart")
    except Exception as e:
        results.append(dict(name="failure", status="fail", error=str(e)))
        raise
    finally:
        if binary is not None and binary.exists():
            for profile in profiles:
                try:
                    cli(profile, "stop", success=False)
                    deadline = time.monotonic() + 10
                    while state(profile)["running"] is not None:
                        if time.monotonic() > deadline:
                            raise RuntimeError("test Runtime remains live")
                        time.sleep(.1)
                except Exception as e:
                    cleanup_ok = False
                    results.append(dict(name="cleanup_runtime", status="fail", error=str(e)))
        if relay is not None:
            relay.terminate()
            relay.wait(timeout=10)
        if relay_log is not None:
            relay_log.close()
        out = ROOT / "dist/verification"
        out.mkdir(parents=True, exist_ok=True)
        if cleanup_ok:
            assert root.parent == Path(tempfile.gettempdir()).resolve() and root.name.startswith("xr-release-")
            assert (root / ".owner").read_text() == "release-e2e"
            assert not any(p.is_symlink() for p in root.rglob("*"))
            shutil.rmtree(root)
        else:
            results.append(dict(name="retained_for_cleanup", path=str(root)))
        (out / "release-lifecycle.json").write_text(json.dumps(results, indent=2), encoding="utf-8")
        if not cleanup_ok:
            raise RuntimeError("test process cleanup requires attention; see release-lifecycle.json")


if __name__ == "__main__":
    main()
