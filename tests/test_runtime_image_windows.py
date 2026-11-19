"""Native Windows image mapping and non-destructive process-watch checks."""
import hashlib
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile
import time
import unittest

from runtime_image_windows import WindowsProcessImage


@unittest.skipUnless(sys.platform == "win32", "requires native Windows image handles")
class WindowsImageTests(unittest.TestCase):
    def start_child(self):
        temporary = tempfile.TemporaryDirectory(prefix="xr-image-watch-")
        self.addCleanup(temporary.cleanup)
        root = Path(temporary.name)
        script = root/"wait.py"
        script.write_text("import os,sys\nfrom pathlib import Path\nPath(sys.argv[1]).write_text(str(os.getpid()))\nsys.stdin.buffer.read(1)\n", encoding="utf-8")
        marker = root/"ready"
        child = subprocess.Popen([sys.executable, str(script), str(marker)], stdin=subprocess.PIPE,
                                 stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL,
                                 creationflags=subprocess.CREATE_NO_WINDOW)
        def cleanup():
            if child.poll() is None:
                child.terminate()  # owned fixture failure cleanup, never the observer
            child.wait(timeout=5)
            child.stdin.close()
        self.addCleanup(cleanup)
        deadline = time.monotonic()+5
        while not marker.exists():
            self.assertIsNone(child.poll(), "native fixture exited before readiness")
            self.assertLess(time.monotonic(), deadline, "native fixture did not become ready")
            time.sleep(.01)
        self.assertEqual(int(marker.read_text()), child.pid)
        selected = Path(sys.executable).resolve()
        data = selected.read_bytes()
        manifest = {"artifact": {"size": len(data), "sha256": hashlib.sha256(data).hexdigest()}}
        return root, selected, manifest, child

    # A byte-identical copy is not the original mapped file object. A checksum
    # or pathname-only observer would wrongly accept this substitution.
    def test_accepts_real_loaded_image_and_rejects_equal_copy_or_wrong_digest(self):
        root, selected, manifest, child = self.start_child()
        copied = root/"same-bytes.exe"
        shutil.copyfile(selected, copied)
        with WindowsProcessImage(child.pid) as process:
            observed = process.observe(selected, manifest)
            self.assertTrue(observed["kernel_image_mapping"])
            self.assertEqual(observed["loaded_sha256"], manifest["artifact"]["sha256"])
            self.assertFalse(process.exited())
            with self.assertRaises(ValueError):
                process.observe(copied, manifest)
            with self.assertRaises(ValueError):
                process.observe(selected, {"artifact": {**manifest["artifact"], "sha256": "0"*64}})

    # Closing a read-only reference must not terminate the live process. A second
    # held handle confirms its later real normal exit, then refuses stale reads.
    def test_close_is_observation_only_and_held_handle_confirms_actual_exit(self):
        _, selected, manifest, child = self.start_child()
        process = WindowsProcessImage(child.pid)
        process.observe(selected, manifest)
        process.close()
        process.close()
        self.assertIsNone(child.poll())
        with self.assertRaises(ValueError):
            process.exited()
        with WindowsProcessImage(child.pid) as held:
            self.assertFalse(held.exited())
            child.stdin.write(b"x")
            child.stdin.flush()
            self.assertEqual(child.wait(timeout=5), 0)
            self.assertTrue(held.exited())
            with self.assertRaises(ProcessLookupError):
                held.observe(selected, manifest)

    # Invalid values must not turn into another process identity via conversion.
    def test_rejects_invalid_pid_before_opening_handle(self):
        for pid in (0, -1, True, "123", 2**32):
            with self.subTest(pid=pid), self.assertRaises(ValueError):
                WindowsProcessImage(pid)

    # A discovery snapshot is only a hint; the retained handle must independently
    # confirm parent and exact arguments before a Runtime role can be asserted.
    def test_observes_parent_and_arguments_and_rejects_wrong_runtime_entry(self):
        from runtime_process_windows import child_process_ids
        root, selected, manifest, child = self.start_child()
        self.assertIn(child.pid, child_process_ids(os.getpid()))
        with WindowsProcessImage(child.pid) as held:
            self.assertEqual(held.parent_pid(), os.getpid())
            args = held.arguments()
            self.assertEqual(Path(args[0]).resolve(), selected)
            self.assertEqual(args[1:], [str(root/"wait.py"), str(root/"ready")])
            with self.assertRaises(ValueError):
                held.observe(selected, {**manifest, "build":{"profile_format":1}}, entry="runtime-serve", expected_parent=os.getpid())
            with self.assertRaises(ValueError):
                held.observe(selected, manifest, expected_parent=os.getpid()+1)


if __name__ == "__main__":
    unittest.main()
