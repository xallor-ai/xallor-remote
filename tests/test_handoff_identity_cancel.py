"""The fault-injection runner must not accept go test's zero-test exit 0."""
import contextlib
import io
import json
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import patch

from tests import handoff_identity_cancel_e2e as runner

CASE = "TestHandoffGateOverlay_shouldRelease_whenCancelledAfterAcquisition"
PACKAGE = "github.com/xallor-ai/xallor-remote/internal/relay"


def event(action, name=CASE):
    return json.dumps({"Action": action, "Test": name, "Package": PACKAGE})


class HandoffGateEvidenceTests(unittest.TestCase):
    def run_result(self, stdout):
        with tempfile.TemporaryDirectory(prefix="xr-gate-report-") as directory:
            output = Path(directory) / "report.json"
            result = subprocess.CompletedProcess([], 0, stdout, "")
            with patch.object(sys, "argv", ["runner", "--output", str(output)]), patch.object(runner.subprocess, "run", return_value=result), contextlib.redirect_stdout(io.StringIO()):
                code = runner.main()
            return code, json.loads(output.read_text(encoding="utf-8"))

    # Exit zero alone also occurs for no matching case, skips and renamed cases.
    def test_rejects_zero_exit_without_the_exact_completed_case(self):
        for stdout in ("", event("pass"), event("run") + "\n" + event("skip"), event("run", "Other") + "\n" + event("pass", "Other")):
            with self.subTest(stdout=stdout):
                code, report = self.run_result(stdout)
                self.assertEqual(code, 1)
                self.assertFalse(report["passed"])

    # A real matched case must both start and finish successfully.
    def test_accepts_the_exact_run_and_pass_events(self):
        code, report = self.run_result(event("run") + "\n" + event("pass"))
        self.assertEqual(code, 0)
        self.assertTrue(report["passed"])
