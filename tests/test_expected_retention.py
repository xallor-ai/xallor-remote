"""Expected crash debt is evidence retention, never product cleanup success."""
import json
from pathlib import Path
import tempfile
from types import SimpleNamespace
import unittest

from fixtures.runtime_cluster import ClusterCleanupError, StopUnconfirmed
from fixtures.expected_retention import verify_expected_retention


class ExpectedRetentionTests(unittest.TestCase):
    def setUp(self):
        self.directory = tempfile.TemporaryDirectory(prefix="xr-cluster-")
        self.addCleanup(self.directory.cleanup)
        self.root = Path(self.directory.name).resolve()
        (self.root/".owner").write_text("runtime-cluster", encoding="utf-8")
        (self.root/"target").mkdir()
        self.before = json.dumps({"version":1,"jobs":{"job":{"state":"uncertain"}}}).encode()
        (self.root/"target/jobs.json").write_bytes(self.before)
        self.cluster = SimpleNamespace(root=self.root, cleanup_complete=False,
                                       evidence={"cryptographic_acceptance":False})
        self.owner = SimpleNamespace(alive=lambda:False)
        self.denial = StopUnconfirmed("target", 1,
            "runtime cleanup is uncertain：任务资源释放尚未确认，请保留现有记录并检查任务进程。\n")
        self.error = ClusterCleanupError(self.root, {"target":self.denial})

    def verify(self, error=None, owner=None, observed_exits=None):
        return verify_expected_retention(self.cluster, error or self.error,
                                         owner or self.owner, self.before,
                                         observed_exits if observed_exits is not None else [True, True],
                                         ["crash_contract"])

    # Expected refusal has its own outcome; it must not certify deletion or
    # production job recovery even when test-owned native processes exited.
    def test_records_expected_refusal_without_claiming_product_cleanup(self):
        result = self.verify()
        self.assertEqual(result["status"], "expected_refusal_verified")
        self.assertFalse(result["cleanup_complete"])
        self.assertTrue(result["test_processes_exited"])
        self.assertEqual((self.root/"target/jobs.json").read_bytes(), self.before)
        self.assertFalse(self.cluster.cleanup_complete)

    # Another profile's failure, or a different target stop error, must not be
    # swallowed as the expected uncertainty from the deliberately crashed jobs.
    def test_rejects_additional_or_unrelated_cleanup_errors(self):
        for failures in ({"target":self.denial,"controller":RuntimeError("stop failed")},
                         {"target":RuntimeError("permission denied")},
                         {"target":StopUnconfirmed("target",1,"source proof failed")},
                         {"target":StopUnconfirmed("target",1,"runtime cleanup is uncertain: stop acknowledgement missing")},
                         {"target":StopUnconfirmed("target",0,"runtime cleanup is uncertain")}):
            with self.subTest(failures=failures), self.assertRaises(AssertionError):
                self.verify(error=ClusterCleanupError(self.root, failures))

    # No PID absence inference: the caller's retained native owner reference
    # and every test-owned child cleanup result must confirm exit.
    def test_rejects_live_owner_or_missing_child_exit_evidence(self):
        with self.assertRaises(AssertionError):
            self.verify(owner=SimpleNamespace(alive=lambda:True))
        for exits in ([], [True,False]):
            with self.assertRaises(AssertionError):
                self.verify(observed_exits=exits)

    # A stopped owner does not authorize rewriting, releasing or replacing the
    # uncertain records. Byte-identical retention is part of this test contract.
    def test_rejects_changed_or_non_uncertain_ledger(self):
        (self.root/"target/jobs.json").write_bytes(b'{"version":1,"jobs":{}}')
        with self.assertRaises(AssertionError):
            self.verify()
        self.before = b'{"version":1,"jobs":{"job":{"state":"reserved"}}}'
        (self.root/"target/jobs.json").write_bytes(self.before)
        with self.assertRaises(AssertionError):
            self.verify()

    # This outcome is only for a completed negative scenario on its owned
    # fixture; it cannot turn an empty run or a deleted root into success.
    def test_rejects_wrong_root_or_missing_scenario(self):
        with self.assertRaises(AssertionError):
            self.verify(error=ClusterCleanupError(self.root/"other", {"target":self.denial}))
        with self.assertRaises(AssertionError):
            verify_expected_retention(self.cluster,self.error,self.owner,self.before,[True],[])


if __name__ == "__main__":
    unittest.main()
