"""Explicit fixture selection and completed evidence boundaries, without processes."""
import argparse
import contextlib
import io
import json
from pathlib import Path
import tempfile
import unittest
from types import SimpleNamespace
from unittest.mock import patch

from fixtures.mechanics_cli import add_mechanics_options, cluster_from_options, completed_report, begin_report


class MechanicsCLITests(unittest.TestCase):
    def parser(self):
        parser = argparse.ArgumentParser()
        parser.add_argument("--binary", default="supplied-runtime")
        add_mechanics_options(parser)
        return parser

    # Default behavior keeps real source verification; only an explicit flag
    # selects a controlled native fixture, never an inherited test environment.
    def test_selects_mechanics_only_from_explicit_cli_option(self):
        for flags, expected in [([], (None, None)),
                                (["--owner-mechanics", "--go", "pinned-go"], ("pinned-go", None)),
                                (["--owner-mechanics-binary", "fixture"], (None, "fixture"))]:
            with patch("fixtures.mechanics_cli.Cluster") as cluster:
                cluster_from_options(self.parser().parse_args(flags))
                cluster.assert_called_once_with("supplied-runtime", mechanics_go=expected[0], mechanics_binary=expected[1])

    # Mixed build/prebuilt selection is ambiguous and must fail before building.
    def test_rejects_conflicting_modes(self):
        with contextlib.redirect_stderr(io.StringIO()), self.assertRaises(SystemExit):
            self.parser().parse_args(["--owner-mechanics", "--owner-mechanics-binary", "fixture"])

    # A passed scenario list does not certify cleanup; no success report is
    # available until Cluster.close completed its owned-resource cleanup.
    def test_report_requires_completed_cleanup_and_keeps_evidence_scope(self):
        cluster = SimpleNamespace(cleanup_complete=False, evidence={"mode":"native-owner-mechanics", "cryptographic_acceptance":False})
        with self.assertRaisesRegex(RuntimeError, "cleanup"):
            completed_report(cluster, [{"name":"example", "status":"pass"}])
        cluster.cleanup_complete = True
        report = completed_report(cluster, [{"name":"example", "status":"pass"}])
        self.assertTrue(report["cleanup_complete"])
        self.assertFalse(report["evidence"]["cryptographic_acceptance"])
        self.assertEqual(report["status"], "passed")

    # A failed rerun must not leave an old passed report looking current.
    def test_begin_report_invalidates_stale_success_and_pins_harness(self):
        cluster = SimpleNamespace(evidence={"cryptographic_acceptance":False})
        with tempfile.TemporaryDirectory() as directory:
            report = Path(directory)/"report.json"
            report.write_text('{"status":"passed"}', encoding="utf-8")
            begin_report(report, cluster, Path(__file__))
            observed = json.loads(report.read_text(encoding="utf-8"))
        self.assertEqual(observed["status"], "incomplete")
        self.assertFalse(observed["cleanup_complete"])
        self.assertEqual(len(observed["evidence"]["harness_sha256"]), 64)

    # A successful exit without any scenarios is not a completed test contract.
    def test_report_rejects_empty_or_failed_scenarios(self):
        cluster = SimpleNamespace(cleanup_complete=True, evidence={})
        for results in ([], [{"name":"broken", "status":"fail"}]):
            with self.assertRaises(ValueError):
                completed_report(cluster, results)

    # A stray string/null/list cannot masquerade as a verified scenario.
    def test_report_rejects_non_object_scenarios(self):
        cluster = SimpleNamespace(cleanup_complete=True, evidence={})
        for invalid in ("pass", None, 1, ["pass"]):
            with self.subTest(invalid=invalid), self.assertRaises(ValueError):
                completed_report(cluster, [{"name":"real", "status":"pass"}, invalid])

    # Even fixture compilation/identity rejection must invalidate an old success
    # before any Runtime is launched or a new Cluster object is returned.
    def test_constructor_failure_cannot_leave_stale_success(self):
        with tempfile.TemporaryDirectory() as directory:
            report = Path(directory)/"report.json"
            report.write_text('{"status":"passed"}', encoding="utf-8")
            with patch("fixtures.mechanics_cli.Cluster", side_effect=RuntimeError("fixture rejected")):
                with self.assertRaisesRegex(RuntimeError, "fixture rejected"):
                    cluster_from_options(self.parser().parse_args([]), report, Path(__file__))
            self.assertEqual(json.loads(report.read_text(encoding="utf-8"))["status"], "incomplete")


if __name__ == "__main__":
    unittest.main()
