"""Real child-process regressions for the opt-in session creation harness."""
from pathlib import Path
import sys
import tempfile
import unittest
from fixtures.cursor_session import create_chat

SESSION = "12345678-1234-1234-1234-123456789abc"


class CursorSessionTests(unittest.TestCase):
    def run_cli(self, source, timeout=3):
        with tempfile.TemporaryDirectory(prefix="xr-session-test-") as directory:
            script = Path(directory) / "agent.py"
            script.write_text(source, encoding="utf-8")
            return create_chat([sys.executable, str(script)], directory, timeout=timeout)

    # An explicit ID remains usable when the allocator lingers; never report job completion.
    def test_should_capture_id_when_allocator_does_not_exit(self):
        session, exited = self.run_cli(f"import time\nprint('{SESSION}', flush=True)\ntime.sleep(30)\n")
        self.assertEqual(session, SESSION)
        self.assertFalse(exited)

    # Normal official CLI behavior keeps its observed successful exit evidence.
    def test_should_preserve_successful_exit(self):
        self.assertEqual(self.run_cli(f"print('{SESSION}')\n"), (SESSION, True))

    # A UUID in arbitrary logs or multiple IDs cannot select a target session.
    def test_should_reject_ambiguous_output(self):
        for output in (f"request-id {SESSION}", f"{SESSION}\n{SESSION}", ""):
            with self.subTest(output=output), self.assertRaises(AssertionError):
                self.run_cli(f"print({output!r})\n")

    # A failing process is not accepted even if it printed an ID before failing.
    def test_should_reject_nonzero_exit(self):
        with self.assertRaises(AssertionError):
            self.run_cli(f"import sys\nprint('{SESSION}')\nsys.exit(1)\n")


if __name__ == "__main__":
    unittest.main()
