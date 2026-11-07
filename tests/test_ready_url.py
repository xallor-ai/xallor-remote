"""A ready-file creation is not proof that the listener address was written."""
from pathlib import Path
import tempfile
import threading
from types import SimpleNamespace
import unittest
from unittest.mock import patch

from fixtures.ready_url import wait_ready_url


class ReadyURLTest(unittest.TestCase):
    # The reader observes an actual empty/partial file before the producer
    # completes it. It must return the final address rather than the prefix.
    def test_should_wait_for_complete_record_after_file_creation(self):
        for initial in ("", "http://127.0.0.1:12"):
            with self.subTest(initial=initial), tempfile.TemporaryDirectory() as directory:
                path = Path(directory) / "ready"
                path.write_text(initial, encoding="utf-8")
                observed = threading.Event()
                result = []
                original = Path.read_text

                def read_record(target, *args, **kwargs):
                    data = original(target, *args, **kwargs)
                    observed.set()
                    return data

                def wait():
                    try:
                        result.append(wait_ready_url(path, SimpleNamespace(poll=lambda: None), timeout=5))
                    except Exception as error:
                        result.append(error)

                with patch.object(Path, "read_text", autospec=True, side_effect=read_record):
                    worker = threading.Thread(target=wait)
                    worker.start()
                    try:
                        self.assertTrue(observed.wait(5), "reader never observed the incomplete file")
                        path.write_text("http://127.0.0.1:12345\n", encoding="utf-8")
                    finally:
                        worker.join(timeout=6)
                self.assertFalse(worker.is_alive())
                self.assertEqual(result, ["http://127.0.0.1:12345"])

    # A stopped fixture cannot make a stale ready file trustworthy.
    def test_should_fail_when_fixture_exits_before_readiness(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "ready"
            path.write_text("http://127.0.0.1:12345\n", encoding="utf-8")
            with self.assertRaisesRegex(RuntimeError, "exited before readiness: 23"):
                wait_ready_url(path, SimpleNamespace(poll=lambda: 23))

    # A live producer that never terminates its record must hit a bounded wait.
    def test_should_time_out_on_unfinished_record(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "ready"
            path.write_text("http://127.0.0.1:12345", encoding="utf-8")
            with self.assertRaises(TimeoutError):
                wait_ready_url(path, SimpleNamespace(poll=lambda: None), timeout=.05)


if __name__ == "__main__":
    unittest.main()
