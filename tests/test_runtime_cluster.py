"""Fixture cleanup contract tests; do not launch a Runtime or delete a profile."""
from pathlib import Path
import os
import subprocess
import unittest
from unittest.mock import patch, Mock

from fixtures.runtime_cluster import Cluster, ClusterCleanupError, StopUnconfirmed


class ClusterCleanupTests(unittest.TestCase):
    # Ordinary clusters must not inherit an opt-in test dispatcher or another
    # profile's credentials. Explicit fixture wiring alone enables mechanics.
    def test_profile_environment_requires_explicit_mechanics_mode(self):
        cluster = object.__new__(Cluster)
        cluster.fixture_env = {}
        cluster.root = Path.cwd()/"unused-profile"
        cluster.url = "ws://127.0.0.1:1/remote"
        with patch.dict(os.environ, {"PATH":"system-tools","XR_TEST_NATIVE_OWNER_FLOW":"1",
                                    "XR_TEST_FAIL_CANDIDATE_SHA":"unrelated",
                                    "XALLOR_REMOTE_DATA_DIR":"unrelated","XALLOR_REMOTE_DEVICE_GRANT":"secret"}, clear=True):
            normal = cluster.env("target")
            self.assertNotIn("XR_TEST_NATIVE_OWNER_FLOW", normal)
            self.assertNotIn("XR_TEST_FAIL_CANDIDATE_SHA", normal)
            self.assertNotIn("XALLOR_REMOTE_DEVICE_GRANT", normal)
            self.assertEqual(normal["XALLOR_REMOTE_DATA_DIR"], str(cluster.root/"target"))
            cluster.fixture_env = {"XR_TEST_NATIVE_OWNER_FLOW":"1","PATH":"owned-verifier"}
            explicit = cluster.env("target")
            self.assertEqual(explicit["XR_TEST_NATIVE_OWNER_FLOW"], "1")
            self.assertEqual(explicit["PATH"], "owned-verifier")

    # Absent IPC after stop cannot override an explicit resource-uncertainty
    # response. Retain the profile instead of allowing close() to delete evidence.
    def test_refuses_cleanup_when_stop_reports_uncertain_even_if_status_is_absent(self):
        cluster = object.__new__(Cluster)
        cluster.root = Path("unused-test-profile")
        calls = []
        def cli(name, *args, **kwargs):
            calls.append(args)
            if args == ("stop",):
                return subprocess.CompletedProcess(args, 1, "", "runtime cleanup is uncertain")
            return subprocess.CompletedProcess(args, 0, '{"running":null}', "")
        cluster.cli = cli
        with self.assertRaisesRegex(RuntimeError, "cleanup is uncertain"):
            cluster.stop("target")
        self.assertEqual(calls, [("stop",)])

    # The intentional-retention assertion must see every failing profile.
    # Cleanup cannot discard a second failure or remove the evidence directory.
    def test_cleanup_retains_typed_errors_for_all_profiles(self):
        cluster = object.__new__(Cluster)
        cluster.root = Path("unused-test-profile")
        cluster.profiles = ["target", "controller"]
        cluster.relay = None
        cluster.log = Mock()
        cluster.cleanup_complete = False
        target = StopUnconfirmed("target", 1, "runtime cleanup is uncertain")
        controller = RuntimeError("unrelated failure")
        cluster.stop = Mock(side_effect=[target, controller])
        with patch("fixtures.runtime_cluster.shutil.rmtree") as delete:
            with self.assertRaises(ClusterCleanupError) as raised:
                cluster.close()
            delete.assert_not_called()
        self.assertEqual(raised.exception.failures, {"target":target,"controller":controller})
        cluster.log.close.assert_called_once()
        self.assertFalse(cluster.cleanup_complete)


if __name__ == "__main__":
    unittest.main()
