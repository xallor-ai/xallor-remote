"""Publication readiness retries cannot hide content or permanent I/O failures."""
import errno
import json
import unittest
from unittest.mock import Mock, patch

from fixtures.publication_read import wait_text


class PublicationReadTests(unittest.TestCase):
    # A Windows rename/open overlap can temporarily deny a reader. Observe
    # the same path until readable without restarting the producer.
    def test_retries_denied_and_missing_reads_until_publication(self):
        path = Mock()
        path.read_text.side_effect = [FileNotFoundError(), PermissionError(errno.EACCES, "denied"), '{"pid":42}']
        with patch("fixtures.publication_read.os.name", "nt"), patch("fixtures.publication_read.time.sleep"):
            self.assertEqual(json.loads(wait_text(path)), {"pid":42})
        self.assertEqual(path.read_text.call_count, 3)

    # A dead producer cannot become ready; observe its result once and fail
    # without sleeping or starting another process.
    def test_fails_immediately_when_producer_exits_before_publication(self):
        path, producer = Mock(), Mock()
        path.read_text.side_effect = FileNotFoundError()
        producer.poll.return_value = 1
        with patch("fixtures.publication_read.time.sleep") as sleep:
            with self.assertRaisesRegex(AssertionError, "exited before startup"):
                wait_text(path, producer)
        producer.communicate.assert_called_once()
        sleep.assert_not_called()

    # A persistently unreadable file must exhaust the original 15-second
    # observation budget and preserve the final access error as its cause.
    def test_fails_when_existing_deadline_expires(self):
        path = Mock()
        denied = PermissionError(errno.EACCES, "denied")
        path.read_text.side_effect = denied
        with patch("fixtures.publication_read.os.name", "nt"), \
                patch("fixtures.publication_read.time.monotonic", side_effect=[0, 15]), \
                patch("fixtures.publication_read.time.sleep") as sleep:
            with self.assertRaises(TimeoutError) as caught:
                wait_text(path)
        self.assertIs(caught.exception.__cause__, denied)
        sleep.assert_not_called()

    # Nontransient filesystem errors, non-Windows permissions, and invalid
    # published content are failures, not reasons to replay or wait again.
    def test_immediately_preserves_nontransient_errors_and_bad_content(self):
        for platform, error in [("nt", OSError(errno.EIO, "I/O")),
                                ("posix", PermissionError(errno.EACCES, "denied")),
                                ("nt", UnicodeDecodeError("utf-8", b"\xff", 0, 1, "invalid"))]:
            path = Mock()
            path.read_text.side_effect = error
            with self.subTest(platform=platform, error=error), \
                    patch("fixtures.publication_read.os.name", platform), \
                    patch("fixtures.publication_read.time.sleep") as sleep:
                with self.assertRaises(type(error)) as caught:
                    wait_text(path)
                self.assertIs(caught.exception, error)
                sleep.assert_not_called()
        path = Mock()
        path.read_text.return_value = "not JSON"
        with self.assertRaises(json.JSONDecodeError):
            json.loads(wait_text(path))
        path.read_text.assert_called_once()


if __name__ == "__main__":
    unittest.main()
