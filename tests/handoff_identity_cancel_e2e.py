"""Deterministic gate-cancellation injection; no product source is modified.

This tests the real handler/gate with a temporary Go overlay at the acquisition
boundary. It is not evidence of a naturally scheduled race or Runtime E2E.
"""
import argparse
import hashlib
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile

ROOT = Path(__file__).resolve().parents[1]
SOURCE = ROOT / "internal/relay/hub_identity.go"
HANDLER = ROOT / "internal/relay/hub_invocation.go"
FIXTURE = ROOT / "tests/fixtures/handoff_gate_cancel_test.go.txt"
CASE = "TestHandoffGateOverlay_shouldRelease_whenCancelledAfterAcquisition"
PACKAGE = "github.com/xallor-ai/xallor-remote/internal/relay"
ANCHOR = "\treturn ctx, func() { release(); cancel() }, nil"
INJECT = '''\tif hook, ok := conn.Context().Value("xr-test-handoff-after-acquire").(func(context.CancelFunc)); ok {
\t\thook(cancel)
\t}
'''


def digest(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--go", default="go")
    parser.add_argument("--output", default="dist/verification/handoff-identity-cancel.json")
    args = parser.parse_args()
    output = Path(args.output).resolve()
    output.parent.mkdir(parents=True, exist_ok=True)
    before = {str(p.relative_to(ROOT)): digest(p) for p in (SOURCE, HANDLER, FIXTURE)}
    report = {"scope": "real Relay handler/gate with post-acquire cancellation overlay", "source_before": before, "passed": False}
    try:
        source = SOURCE.read_text(encoding="utf-8")
        if source.count(ANCHOR) != 1:
            raise ValueError("identity acquisition boundary changed; review fault injection")
        with tempfile.TemporaryDirectory(prefix="xr-handoff-gate-") as directory:
            work = Path(directory)
            replacement = work / "hub_identity.go"
            replacement.write_text(source.replace(ANCHOR, INJECT + ANCHOR), encoding="utf-8")
            overlay = work / "overlay.json"
            overlay.write_text(json.dumps({"Replace": {
                str(SOURCE): str(replacement),
                str(ROOT / "internal/relay/zz_handoff_gate_cancel_overlay_test.go"): str(FIXTURE),
            }}), encoding="utf-8")
            env = dict(os.environ, GOMAXPROCS="2")
            result = subprocess.run([args.go, "test", "-json", "-overlay", str(overlay), "-count=1", "-timeout=30s", "-run=^" + CASE + "$", "./internal/relay"], cwd=ROOT, env=env, capture_output=True, encoding="utf-8", errors="replace", timeout=180)
            report.update(exit_code=result.returncode, stdout=result.stdout, stderr=result.stderr)
            print(result.stdout + result.stderr, flush=True)
        report["temporary_directory_removed"] = not Path(directory).exists()
        report["source_after"] = {str(p.relative_to(ROOT)): digest(p) for p in (SOURCE, HANDLER, FIXTURE)}
        events = [json.loads(line) for line in result.stdout.splitlines() if line.strip()]
        actions = [event.get("Action") for event in events
                   if event.get("Test") == CASE and event.get("Package") == PACKAGE
                   and event.get("Action") != "output"]
        report["required_case"] = {"name": CASE, "actions": actions}
        report["passed"] = result.returncode == 0 and actions == ["run", "pass"] and report["source_after"] == before and report["temporary_directory_removed"]
    except Exception as error:
        report["error"] = type(error).__name__ + ": " + str(error)
    output.write_text(json.dumps(report, indent=2), encoding="utf-8")
    print("REPORT", output, flush=True)
    return 0 if report["passed"] else 1


if __name__ == "__main__":
    sys.stdout.reconfigure(encoding="utf-8", errors="replace")
    sys.exit(main())
