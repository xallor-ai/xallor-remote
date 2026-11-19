"""The acceptance report must never overwrite executable test inputs."""
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[1]


class ObserverInputProtectionTests(unittest.TestCase):
    # Invalid non-executable fixtures keep the pre-fix reproducer inert; any
    # constructor scratch directories stay inside this test-owned TEMP root.
    def test_rejects_output_colliding_with_observer_or_fixture_before_writing(self):
        for name in ("observer", "fixture"):
            with self.subTest(name=name), tempfile.TemporaryDirectory() as directory:
                root = Path(directory)
                observer, fixture = root/"observer", root/"fixture"
                original = b"non-executable original input bytes"
                observer.write_bytes(original)
                fixture.write_bytes(original)
                result = subprocess.run([sys.executable, str(ROOT/"tests/runtime_observer_e2e.py"),
                                         "--observer", str(observer), "--owner-mechanics-binary", str(fixture),
                                         "--output", str(root/name)], cwd=ROOT,
                                        env=dict(os.environ, TMP=str(root), TEMP=str(root), TMPDIR=str(root)),
                                        capture_output=True, encoding="utf-8", errors="replace", timeout=20)
                self.assertNotEqual(result.returncode, 0)
                self.assertEqual(observer.read_bytes(), original)
                self.assertEqual(fixture.read_bytes(), original)


if __name__ == "__main__":
    unittest.main()
