"""Two genuine signed versions through the real installer on an isolated profile.
No production Relay, default profile, mock verifier or fake process controller.
"""
import argparse
from contextlib import closing
import hashlib
import json
import os
from pathlib import Path
import shutil
import sqlite3
import subprocess
import sys
import time
from fixtures.signed_cycle_report import SignedCycleReport
from fixtures.runtime_observation_set import RuntimeObservations, require_replacement
from fixtures.runtime_ready_observation import query_owner
from fixtures.signed_startup_fault import StartupFaultUnconfirmed
from fixtures.restore_evidence import identity as selected_identity


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--installer", required=True)
    parser.add_argument("--first", required=True)
    parser.add_argument("--second", required=True)
    parser.add_argument("--output", required=True)
    parser.add_argument("--observer", help="Required trusted tests/fixtures/runtime-observer native helper")
    parser.add_argument("--startup-fault", action="store_true", help="Cause a real candidate startup failure using the owned identity DB")
    args = parser.parse_args()
    output = Path(args.output).resolve()
    protected = [Path(args.first).resolve(), Path(args.second).resolve()]
    if output == Path(args.installer).resolve() or (args.observer and output == Path(args.observer).resolve()) or any(output == p or p in output.parents for p in protected):
        parser.error("--output must be outside candidate bundles and must not overwrite the installer")
    cycle = SignedCycleReport(output)
    failure = None
    try:
        if args.startup_fault and sys.platform!="win32" and not sys.platform.startswith("linux"):
            parser.error("--startup-fault requires Windows or Linux native process observation")
        run_cycle(args, cycle)
    except BaseException as error:
        failure = error
        raise
    finally:
        cycle.finish(failure)


def run_cycle(args, cycle):
    if not args.observer:
        raise ValueError("--observer is required for native owner/launch evidence")
    observer = Path(args.observer).resolve()
    installer = Path(args.installer).resolve()
    bundles = [Path(args.first).resolve(), Path(args.second).resolve()]
    manifest_bytes = [(p/"manifest.json").read_bytes() for p in bundles]
    manifests = [json.loads(raw) for raw in manifest_bytes]
    original_manifests = {m["artifact"]["sha256"]: raw for m, raw in zip(manifests, manifest_bytes)}
    assert manifests[0]["artifact"]["sha256"] != manifests[1]["artifact"]["sha256"]
    work = cycle.create_workspace()
    profile = work/"profile"
    workspace = work/"workspace"
    profile.mkdir(); workspace.mkdir()
    (profile/"config.json").write_text(json.dumps({
        "relay_url": "ws://127.0.0.1:1/remote", "workspace": str(workspace)}), encoding="utf-8")
    sentinel = workspace/"user-data.txt"
    sentinel.write_text("must survive upgrades 中文", encoding="utf-8")
    env = os.environ.copy()
    for key in list(env):
        if key.upper().startswith("XALLOR_"): env.pop(key)
    env.update(XALLOR_REMOTE_DATA_DIR=str(profile), XALLOR_REMOTE_RELAY_URL="ws://127.0.0.1:1/remote")
    report = cycle.data
    results = report["results"]
    observations = RuntimeObservations(report, lambda: query_owner(observer, profile, env))
    report.update(versions=[m["build"] for m in manifests], installer_sha256=hashlib.sha256(installer.read_bytes()).hexdigest(), observer_sha256=hashlib.sha256(observer.read_bytes()).hexdigest())
    def cli(*words):
        cycle.execution_started = True
        kwargs = {"creationflags": subprocess.CREATE_NO_WINDOW} if os.name == "nt" else {}
        return subprocess.run([str(installer), *words], env=env, capture_output=True,
                              text=True, encoding="utf-8", errors="replace", timeout=150, **kwargs)
    def status():
        p = cli("runtime", "status")
        assert p.returncode == 0, p.stderr
        return json.loads(p.stdout)
    def identity():
        with closing(sqlite3.connect((profile/"identity.db").as_uri()+"?mode=ro", uri=True)) as db:
            row = db.execute("SELECT device_id,secret,grant,inbound,active FROM identity WHERE singleton=1").fetchone()
            assert row is not None
            return hashlib.sha256(json.dumps(row).encode()).hexdigest()
    def check_running(m, missing_manifest=False):
        state = status()
        running = state["running"]
        assert running and running["pid"] > 0, state
        for field in ("version", "revision", "protocol", "profile_format", "os", "arch"):
            assert running[field] == m["build"][field], (field, running)
        assert running["sha256"] == m["artifact"]["sha256"]
        state_path = profile/"runtime/state.json"
        with state_path.open("rb") as state_file:
            installed_bytes = state_file.read(65537)
        assert len(installed_bytes) <= 65536, "Installation state exceeds observation limit"
        installed = json.loads(installed_bytes)
        observed = observations.capture(
            profile, installed, m, running,
            None if missing_manifest else original_manifests[m["artifact"]["sha256"]])
        with state_path.open("rb") as state_file:
            assert state_file.read(65537) == installed_bytes, "Installation state changed during image observation"
        return observed
    def record(name, observed):
        results.append({"name": name, "status": "pass", "running": observed.running,
                        "image_observation": observed.evidence})
        print(name, "PASS", flush=True)
    try:
        # Authenticate both original bundles before any candidate can execute.
        for bundle in bundles:
            p = cli("runtime", "verify", "--bundle", str(bundle))
            assert p.returncode == 0 and json.loads(p.stdout)["verified"], p.stderr
        p = cli("runtime", "install", "--bundle", str(bundles[0]))
        assert p.returncode == 0, p.stderr
        first = check_running(manifests[0])
        original_identity = identity()
        config = (profile/"config.json").read_bytes()
        record("first_signed_install", first)
        # Strict policy: losing the running baseline's original manifest must
        # refuse upgrade before stopping it or changing the selected state.
        current_manifest = Path(first.evidence["activation_path"]).parent/"manifest.json"
        held_manifest = current_manifest.with_name("manifest.test-held")
        before_state = (profile/"runtime/state.json").read_bytes()
        before_versions = sorted(p.name for p in (profile/"runtime/versions").iterdir())
        current_manifest.rename(held_manifest)
        try:
            refused = cli("runtime", "install", "--bundle", str(bundles[1]))
            assert refused.returncode != 0
            retained = check_running(manifests[0], missing_manifest=True)
            assert retained.reference is first.reference and identity() == original_identity
            assert (profile/"runtime/state.json").read_bytes() == before_state
            assert sorted(p.name for p in (profile/"runtime/versions").iterdir()) == before_versions
            record("missing_baseline_manifest_refused_before_stop", retained)
        finally:
            held_manifest.rename(current_manifest)
        p = cli("runtime", "install", "--bundle", str(bundles[1]))
        assert p.returncode == 0, p.stderr
        second = check_running(manifests[1])
        observations.require_exit(first)
        require_replacement(first, second)
        assert identity() == original_identity
        assert (profile/"config.json").read_bytes() == config
        record("signed_upgrade_preserves_identity_config", second)
        p = cli("runtime", "rollback")
        assert p.returncode == 0, p.stderr
        restored = check_running(manifests[0])
        observations.require_exit(second)
        require_replacement(second, restored)
        assert identity() == original_identity
        assert (profile/"config.json").read_bytes() == config
        assert sentinel.read_text(encoding="utf-8") == "must survive upgrades 中文"
        record("signed_rollback_preserves_identity_config_files", restored)

        changed = work/"tampered"
        shutil.copytree(bundles[1], changed)
        m = json.loads((changed/"manifest.json").read_bytes())
        candidate = changed/m["artifact"]["filename"]
        with candidate.open("ab") as f: f.write(b"tampered and rehashed")
        m["artifact"].update(size=candidate.stat().st_size,
                             sha256=hashlib.sha256(candidate.read_bytes()).hexdigest())
        (changed/"manifest.json").write_text(json.dumps(m), encoding="utf-8")
        state_bytes = (profile/"runtime/state.json").read_bytes()
        versions = sorted(p.name for p in (profile/"runtime/versions").iterdir())
        p = cli("runtime", "install", "--bundle", str(changed))
        assert p.returncode != 0
        unchanged = check_running(manifests[0])
        assert unchanged.reference is restored.reference and identity() == original_identity
        assert (profile/"runtime/state.json").read_bytes() == state_bytes
        assert sorted(p.name for p in (profile/"runtime/versions").iterdir()) == versions
        record("rehashed_upgrade_refused_without_stopping_current", unchanged)
        if args.startup_fault:
            from fixtures.signed_startup_fault import fail_start_with_database_lock
            before_fault = check_running(manifests[0])
            failure = fail_start_with_database_lock(installer, bundles[1], profile, env, manifests[1], observer=observer,
                                                    baseline_state=json.loads((profile/"runtime/state.json").read_bytes()), baseline=before_fault)
            recovered = check_running(manifests[0])
            observations.require_exit(before_fault)
            require_replacement(before_fault, recovered)
            restored_state = json.loads((profile/"runtime/state.json").read_bytes())
            assert selected_identity(restored_state, "current") == failure["restoration_target"] and not restored_state.get("pending")
            assert failure["durable_restore_confirmed"] and failure["candidate_kernel_exit"] and failure["worker_exited"]
            assert identity() == original_identity
            assert (profile/"config.json").read_bytes() == config
            assert sentinel.read_text(encoding="utf-8") == "must survive upgrades 中文"
            report["startup_fault"] = failure
            record("failed_signed_candidate_restores_previous_process", recovered)
    except BaseException as error:
        if isinstance(error, StartupFaultUnconfirmed):
            report["startup_fault_observation"] = error.observation
            report["cleanup_errors"].append("Startup fault resources/evidence unconfirmed; retain profile for original-operation inspection")
        report["failure"] = {"type": type(error).__name__, "detail": str(error)}
        raise
    finally:
        try:
            stopped = cli("stop")
            assert stopped.returncode == 0, stopped.stderr
            deadline = time.monotonic()+15
            while status()["running"] is not None:
                assert time.monotonic() < deadline, "Owned Runtime did not stop"
                time.sleep(.1)
        except Exception as error:
            report["cleanup_errors"].append(str(error))
        try:
            observations.close_after_exit()
        except Exception as error:
            report["cleanup_errors"].append(str(error))
        if not report["cleanup_errors"]:
            try:
                cycle.remove_workspace()
            except Exception as error:
                report["cleanup_errors"].append(str(error))
        report["passed"] = len(results) == (6 if args.startup_fault else 5) and "failure" not in report and not report["cleanup_errors"]
        if report["cleanup_errors"]: raise RuntimeError("Owned test profile cleanup incomplete")


if __name__ == "__main__":
    sys.stdout.reconfigure(encoding="utf-8", errors="replace")
    main()

