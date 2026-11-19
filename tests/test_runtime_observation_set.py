"""Bookkeeping gates; native kernel behavior is tested separately per OS."""
from unittest import TestCase, main, mock

from fixtures.runtime_observation_set import RuntimeObservations, require_replacement


class Reference:
    def __init__(self, pid, alive=True):
        self.pid, self.alive, self.closed = pid, alive, False

    def exited(self):
        if self.closed:
            raise ValueError("closed")
        return not self.alive

    def close(self):
        self.closed = True


class ObservationSetTests(TestCase):
    def setUp(self):
        self.report = {}
        self.ready = mock.Mock(side_effect=self.ready_view)
        self.ledger = RuntimeObservations(self.report, self.ready)
        self.manifest = {"artifact": {"sha256": "a"*64}, "build": {"version": "0.2.6", "revision": "a"*40, "protocol": 1, "profile_format": 1}}
        self.state = {"current_launch_id": "b"*32, "current_activation": "d"*32}
        self.running = {"pid": 123}
        self.factory = self.enterContext(mock.patch("fixtures.runtime_observation_set.open_process_image", side_effect=Reference))
        self.observe = self.enterContext(mock.patch("fixtures.runtime_observation_set.observe_current_image",
                                                  side_effect=lambda *_args, **_kw: {"manifest_present": _args[4] is not None}))

    def ready_view(self):
        return {"peer_pid":123, "runtime": {"state":"ready", "pid":123,
                **self.manifest["build"], "sha256":self.manifest["artifact"]["sha256"],
                "activation_id":self.state["current_activation"], "launch_id":self.state["current_launch_id"]}}

    def capture(self, state=None):
        if state is not None:
            self.state = {**self.state, **state}
        return self.ledger.capture("profile", self.state, self.manifest, self.running, b"manifest")

    # A negative missing-manifest phase cannot rewrite prior acceptance evidence.
    def test_retains_one_handle_and_separate_phase_snapshots(self):
        first = self.capture()
        second = self.ledger.capture("profile", self.state, self.manifest, self.running, None)
        self.assertIs(first.reference, second.reference)
        self.assertTrue(first.evidence["manifest_present"])
        self.assertFalse(second.evidence["manifest_present"])
        self.assertEqual(self.factory.call_count, 1)
        first.reference.alive = False
        self.ledger.require_exit(first)
        self.ledger.close_after_exit(timeout=0)
        self.assertTrue(first.row["kernel_exit"])
        self.assertTrue(first.row["reference_closed"])

    # A stop command/IPC absence cannot substitute for a held live process.
    def test_live_process_blocks_success_without_being_killed(self):
        first = self.capture()
        with self.assertRaises(RuntimeError):
            self.ledger.require_exit(first)
        with self.assertRaises(RuntimeError):
            self.ledger.close_after_exit(timeout=0)
        self.assertTrue(first.reference.alive)
        self.assertTrue(first.reference.closed)
        self.assertFalse(first.row["kernel_exit"])

    # Linux can reuse a stopped numeric PID; the old held reference stays distinct.
    def test_reused_pid_after_exit_keeps_two_distinct_references(self):
        first = self.capture()
        first.reference.alive = False
        second = self.capture({"current_launch_id": "c"*32})
        self.assertIsNot(first.reference, second.reference)
        self.ledger.require_exit(first)
        self.assertTrue(second.reference.alive)
        second.reference.alive = False
        self.ledger.close_after_exit(timeout=0)
        self.assertEqual(len(self.report["process_observations"]), 2)

    # The first capture requires a matching native-peer ready reply, not just a
    # disk nonce; missing or contradictory live launch identity must fail.
    def test_first_capture_rejects_missing_or_mismatched_ready_identity(self):
        for field, wrong in (("launch_id", "c"*32), ("activation_id", "e"*32), ("pid", 124), ("launch_id", None)):
            with self.subTest(field=field, wrong=wrong):
                report = {}
                view = self.ready_view()
                view["runtime"][field] = wrong
                ledger = RuntimeObservations(report, lambda: view)
                with self.assertRaises(ValueError):
                    ledger.capture("profile", self.state, self.manifest, self.running, b"manifest")
                for entry in ledger.entries.values():
                    entry.reference.alive = False
                with self.assertRaises(RuntimeError):
                    ledger.close_after_exit(timeout=0)

    # Numerical PID equality is permitted only for independently held old/new
    # references with different launches and an exited old/live new process.
    def test_replacement_accepts_pid_reuse_but_not_a_live_previous_launch(self):
        first = self.capture()
        first.reference.alive = False
        second = self.capture({"current_launch_id":"c"*32})
        require_replacement(first, second)
        first.reference.alive = True
        with self.assertRaises(RuntimeError):
            require_replacement(first, second)
        first.reference.alive = False
        second.reference.alive = False
        self.ledger.close_after_exit(timeout=0)

    # A still-live object cannot be relabeled as a replacement launch.
    def test_changed_launch_on_live_pid_is_uncertain(self):
        first = self.capture()
        with self.assertRaises(ValueError):
            self.capture({"current_launch_id": "c"*32})
        first.reference.alive = False
        with self.assertRaises(RuntimeError):
            self.ledger.close_after_exit(timeout=0)
        self.assertTrue(first.reference.closed)

    # Register before image inspection so a failure still releases its reference,
    # but an incomplete binding never authorizes deleting the evidence root.
    def test_failed_image_binding_closes_reference_but_remains_uncertain(self):
        reference = Reference(123, alive=False)
        self.factory.side_effect = None
        self.factory.return_value = reference
        self.observe.side_effect = ValueError("image mismatch")
        with self.assertRaises(ValueError):
            self.capture()
        with self.assertRaises(RuntimeError):
            self.ledger.close_after_exit(timeout=0)
        self.assertTrue(reference.closed)
        self.assertFalse(self.report["process_observations"][0]["image_bound"])

    # A native CloseHandle error is retained, not reported as successful release.
    def test_close_failure_prevents_cleanup_confirmation(self):
        first = self.capture()
        first.reference.alive = False
        with mock.patch.object(first.reference, "close", side_effect=OSError("close failed")):
            with self.assertRaises(RuntimeError):
                self.ledger.close_after_exit(timeout=0)
        self.assertFalse(first.row["reference_closed"])


if __name__ == "__main__":
    main()
