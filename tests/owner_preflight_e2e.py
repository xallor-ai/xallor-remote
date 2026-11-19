"""Real signed legacy bundle refusal at the Runtime owner capability boundary.

Uses the current installer and actual system gh, in an isolated profile with
no production Relay. This is refusal evidence, not a successful upgrade cycle.
"""
import argparse
import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--installer", required=True)
    parser.add_argument("--legacy-bundle", required=True)
    parser.add_argument("--output", required=True)
    args = parser.parse_args()
    installer = Path(args.installer).resolve()
    bundle = Path(args.legacy_bundle).resolve()
    manifest = json.loads((bundle/"manifest.json").read_bytes())
    work = Path(tempfile.mkdtemp(prefix="xr-owner-preflight-")).resolve()
    profile = work/"profile"
    report = {"scope": "real provenance plus legacy owner preflight refusal",
              "installer_sha256": hashlib.sha256(installer.read_bytes()).hexdigest(),
              "candidate_sha256": manifest["artifact"]["sha256"],
              "passed": False, "cleanup_errors": []}
    env = {k: v for k, v in os.environ.items() if not k.upper().startswith("XALLOR_")}
    env.update(XALLOR_REMOTE_DATA_DIR=str(profile),
               XALLOR_REMOTE_RELAY_URL="ws://127.0.0.1:1/remote")

    def cli(*words):
        options = {"creationflags": subprocess.CREATE_NO_WINDOW} if os.name == "nt" else {}
        return subprocess.run([str(installer), *words], env=env, text=True,
                              encoding="utf-8", errors="replace", capture_output=True,
                              timeout=150, **options)

    try:
        verified = cli("runtime", "verify", "--bundle", str(bundle))
        assert verified.returncode == 0, verified.stderr
        source = json.loads(verified.stdout)
        assert source["verified"] and source["artifact"]["sha256"] == report["candidate_sha256"]
        report["source_verification"] = source
        result = cli("runtime", "install", "--bundle", str(bundle))
        assert result.returncode != 0, "Legacy bundle unexpectedly activated"
        assert "生命周期协议" in result.stderr or "owner protocol" in result.stderr, result.stderr
        status = cli("runtime", "status")
        assert status.returncode == 0, status.stderr
        observed = json.loads(status.stdout)
        assert observed["running"] is None and not observed["installed"], observed
        assert not (profile/"identity.db").exists(), "Candidate initialized an identity"
        assert not (profile/"jobs.json").exists(), "Candidate initialized jobs"
        activation_root = profile/"runtime/activations"
        assert not activation_root.exists() or not list(activation_root.iterdir()), "Rejected activation leaked"
        report.update(passed=True, install_exit=result.returncode,
                      diagnostic=result.stderr.strip(), status=observed)
    except BaseException as error:
        report["failure"] = str(error)
        raise
    finally:
        try:
            stopped = cli("stop")
            assert stopped.returncode == 0, stopped.stderr
            observed = cli("runtime", "status")
            assert observed.returncode == 0 and json.loads(observed.stdout)["running"] is None
            assert work.parent == Path(tempfile.gettempdir()).resolve()
            assert work.name.startswith("xr-owner-preflight-")
            shutil.rmtree(work)
        except Exception as error:
            report["cleanup_errors"].append(str(error))
            report["passed"] = False
        output = Path(args.output).resolve()
        output.parent.mkdir(parents=True, exist_ok=True)
        output.write_text(json.dumps(report, ensure_ascii=False, indent=2), encoding="utf-8")
        print(json.dumps({"passed": report["passed"], "cleanup_errors": report["cleanup_errors"],
                          "evidence": str(output)}, ensure_ascii=False))
        if report["cleanup_errors"]:
            raise RuntimeError("Owned preflight test cleanup incomplete")


if __name__ == "__main__":
    sys.stdout.reconfigure(encoding="utf-8", errors="replace")
    main()
