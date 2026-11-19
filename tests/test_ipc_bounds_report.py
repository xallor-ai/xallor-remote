"""Exercise the IPC harness reporting path without claiming native acceptance."""
import contextlib
import io
import json
from pathlib import Path
import tempfile
from types import SimpleNamespace
import unittest
from unittest.mock import Mock, patch

import ipc_bounds_e2e as harness


class IPCBoundsReportTests(unittest.TestCase):
    # Once the harness assertions and cleanup succeed, its actual report must
    # satisfy the shared strict scenario schema instead of passing bare strings.
    def test_reports_scenarios_only_after_cleanup(self):
        cluster = Mock(binary="fixture", evidence={}, cleanup_complete=False)
        cluster.cli.side_effect = [SimpleNamespace(stdout='{"running":{"pid":42}}'),
                                  SimpleNamespace(stdout='{"running":{"pid":42}}'),
                                  SimpleNamespace(stdout='{"online":true}')]
        cluster.close.side_effect = lambda: setattr(cluster, "cleanup_complete", True)
        bridge = Mock(stdin=io.BytesIO())
        bridge.poll.return_value = 0
        with tempfile.TemporaryDirectory() as root, \
                patch.object(harness, "ROOT", Path(root)), \
                patch.object(harness, "cluster_from_options", return_value=cluster), \
                patch.object(harness.subprocess, "Popen", return_value=bridge), \
                patch("sys.argv", ["ipc_bounds_e2e.py"]), \
                contextlib.redirect_stdout(io.StringIO()):
            harness.main()
            report = json.loads((Path(root)/"dist/verification/ipc-bounds.json").read_text())
        self.assertTrue(report["cleanup_complete"])
        self.assertEqual(report["status"], "passed")
        self.assertEqual(len(report["results"]), 2)
        self.assertTrue(all(row["status"] == "pass" for row in report["results"]))


if __name__ == "__main__":
    unittest.main()
