"""Input failures must replace an old signed-lifecycle success report."""
import json
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest
from unittest import mock

from tests.fixtures.signed_cycle_report import SignedCycleReport

ROOT = Path(__file__).resolve().parents[1]


class SignedUpgradeReportTests(unittest.TestCase):
    # Bad inputs fail before any installer is executed, but still produce a
    # current failed report rather than leaving a previous acceptance green.
    def test_missing_or_malformed_manifest_replaces_previous_success(self):
        for malformed in (False, True):
            with self.subTest(malformed=malformed), tempfile.TemporaryDirectory() as directory:
                root = Path(directory)
                first, second = root/"first", root/"second"
                first.mkdir()
                second.mkdir()
                if malformed:
                    (first/"manifest.json").write_text("invalid JSON", encoding="utf-8")
                output = root/"report.json"
                output.write_text(json.dumps({"passed": True, "results": [{"name": "stale", "status": "pass"}]}), encoding="utf-8")
                result = subprocess.run([sys.executable, str(ROOT/"tests/signed_upgrade_e2e.py"),
                                         "--installer", str(root/"absent-installer"), "--observer", str(root/"observer"), "--first", str(first),
                                         "--second", str(second), "--output", str(output)],
                                        capture_output=True, encoding="utf-8", errors="replace", timeout=20)
                self.assertNotEqual(result.returncode, 0)
                report = json.loads(output.read_text(encoding="utf-8"))
                self.assertIs(report["passed"], False)
                self.assertEqual(report["results"], [])
                self.assertIn("failure", report)

    # Setup failure after mkdtemp is still tracked before any process executes.
    def test_failed_workspace_marker_is_cleaned_and_reported(self):
        with tempfile.TemporaryDirectory() as directory:
            cycle = SignedCycleReport(Path(directory)/"report.json")
            original = Path.write_text
            def fail_marker(path, *args, **kwargs):
                if path.name == ".owner":
                    raise PermissionError("injected marker write failure")
                return original(path, *args, **kwargs)
            with mock.patch.object(Path, "write_text", fail_marker):
                with self.assertRaises(PermissionError) as caught:
                    cycle.create_workspace()
            cycle.finish(caught.exception)
            report = json.loads(cycle.output.read_text(encoding="utf-8"))
            self.assertFalse(report["passed"])
            self.assertTrue(report["workspace_created"])
            self.assertTrue(report["workspace_removed"])
            self.assertEqual(report["cleanup_errors"], [])
            self.assertFalse(cycle.work.exists())

    # Exercise the real script's later preparation failure, after config and
    # workspace creation but before the first installer command can run.
    def test_missing_installer_cleans_prepared_workspace(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            for name, digest in (("first", "a"), ("second", "b")):
                bundle = root/name
                bundle.mkdir()
                (bundle/"manifest.json").write_text(json.dumps({"artifact": {"sha256": digest*64}, "build": {}}), encoding="utf-8")
            output = root/"report.json"
            result = subprocess.run([sys.executable, str(ROOT/"tests/signed_upgrade_e2e.py"),
                                     "--installer", str(root/"absent"), "--observer", str(root/"observer"), "--first", str(root/"first"),
                                     "--second", str(root/"second"), "--output", str(output)],
                                    capture_output=True, encoding="utf-8", errors="replace", timeout=20)
            self.assertNotEqual(result.returncode, 0)
            report = json.loads(output.read_text(encoding="utf-8"))
            self.assertFalse(report["passed"])
            self.assertTrue(report["workspace_created"])
            self.assertTrue(report["workspace_removed"])
            self.assertEqual(report["cleanup_errors"], [])
            self.assertFalse(Path(report["owned_workspace"]).exists())

    # Once execution begins, outer error handling cannot infer process cleanup
    # from missing output or silently remove its evidence directory.
    def test_started_cycle_requires_explicit_lifecycle_cleanup(self):
        with tempfile.TemporaryDirectory() as directory:
            cycle = SignedCycleReport(Path(directory)/"report.json")
            cycle.create_workspace()
            try:
                cycle.execution_started = True
                cycle.data["passed"] = True
                cycle.finish()
                report = json.loads(cycle.output.read_text(encoding="utf-8"))
                self.assertFalse(report["passed"])
                self.assertFalse(report["workspace_removed"])
                self.assertTrue(report["cleanup_errors"])
                self.assertTrue(cycle.work.exists())
            finally:
                # This unit creates no process; reclaim only its owned workspace.
                cycle.remove_workspace()

    # Resetting a report early must never overwrite a candidate manifest.
    def test_report_must_not_overwrite_candidate_inputs(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            first = root/"first"
            first.mkdir()
            manifest = first/"manifest.json"
            manifest.write_text("original signed manifest bytes", encoding="utf-8")
            result = subprocess.run([sys.executable, str(ROOT/"tests/signed_upgrade_e2e.py"),
                                     "--installer", str(root/"installer"), "--observer", str(root/"observer"), "--first", str(first),
                                     "--second", str(root/"second"), "--output", str(manifest)],
                                    capture_output=True, encoding="utf-8", errors="replace", timeout=20)
            self.assertNotEqual(result.returncode, 0)
            self.assertEqual(manifest.read_text(encoding="utf-8"), "original signed manifest bytes")


if __name__ == "__main__":
    unittest.main()
