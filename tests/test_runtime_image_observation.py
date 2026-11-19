import hashlib
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest
from unittest import mock

from runtime_image_observation import (LinuxProcessImage, hash_open_image, memfd_label,
                                       observe_current_image, reported_location_matches,
                                       selected_activation)


class ReportedLocationTests(unittest.TestCase):
    # Linux deliberately reports its loaded memfd rather than a disk alias.
    def test_matches_selected_memfd_label_and_rejects_other_label(self):
        with tempfile.TemporaryDirectory() as directory:
            selected = Path(directory)/"activation"/"xallor-remote"
            label = "/memfd:xr-runtime-"+hashlib.sha256(str(selected.absolute()).encode()).hexdigest()
            self.assertTrue(reported_location_matches(label, selected, "linux"))
            self.assertFalse(reported_location_matches("/memfd:xr-runtime-"+"0"*64, selected, "linux"))

    # A normal disk path cannot satisfy sealed-image acceptance on Linux.
    def test_rejects_linux_disk_path_even_when_it_equals_selection(self):
        with tempfile.TemporaryDirectory() as directory:
            selected = Path(directory)/"xallor-remote"
            self.assertFalse(reported_location_matches(str(selected), selected, "linux"))


class ActivationFilesTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.profile = Path(self.temporary.name)
        self.data = b"retained fixture bytes"
        self.manifest = {"build": {"profile_format": 1}, "artifact": {
            "filename": "xallor-remote", "size": len(self.data),
            "sha256": hashlib.sha256(self.data).hexdigest()}}
        self.state = {"current": self.manifest, "current_activation": "a"*32}
        self.selected = self.profile/"runtime/activations"/("a"*32)/"xallor-remote"
        self.selected.parent.mkdir(parents=True)
        self.selected.write_bytes(self.data)
        self.raw = json.dumps(self.manifest).encode()+b"\n"
        (self.selected.parent/"manifest.json").write_bytes(self.raw)

    # Missing or traversing installation references cannot select arbitrary files.
    def test_requires_complete_confined_selection(self):
        self.assertEqual(selected_activation(self.profile, self.state, self.manifest), self.selected.resolve())
        for activation in ("", "../outside", "a"*31):
            with self.assertRaises(ValueError):
                selected_activation(self.profile, {**self.state, "current_activation": activation}, self.manifest)
        with self.assertRaises(ValueError):
            selected_activation(self.profile, {**self.state, "current": {}}, self.manifest)

    # A valid reported digest cannot hide modified retained bytes or size.
    def test_hashes_actual_bounded_retained_bytes(self):
        with self.selected.open("rb") as image:
            self.assertEqual(hash_open_image(image, self.manifest["artifact"]), self.manifest["artifact"]["sha256"])
        self.selected.write_bytes(b"x"*len(self.data))
        with self.selected.open("rb") as image, self.assertRaises(ValueError):
            hash_open_image(image, self.manifest["artifact"])

    # A matching self-reported path/hash must not bind an unrelated live PID
    # to a plain fixture file that is not that process's loaded image.
    @unittest.skipUnless(sys.platform == "win32", "requires Windows image mapping")
    def test_rejects_unrelated_process_despite_matching_reported_path_and_hash(self):
        running = {"pid": os.getpid(), "runtime_owner_protocol": 1,
                   "path": str(self.selected), "sha256": self.manifest["artifact"]["sha256"]}
        with self.assertRaises(ValueError):
            observe_current_image(self.profile, self.state, self.manifest, running, self.raw)

    # Retained-file unit checks isolate the kernel observer; the unrelated-PID
    # regression above and WindowsImageTests exercise real kernel mapping.
    @unittest.skipUnless(sys.platform == "win32", "Windows retained-file branch")
    @mock.patch("runtime_image_observation.open_process_image")
    def test_preserves_raw_manifest_and_explicit_missing_manifest_evidence(self, factory):
        factory.return_value.__enter__.return_value.observe.return_value = {"kernel_image_mapping": True}
        running = {"pid": os.getpid(), "runtime_owner_protocol": 1,
                   "path": str(self.selected), "sha256": self.manifest["artifact"]["sha256"]}
        observed = observe_current_image(self.profile, self.state, self.manifest, running, self.raw)
        self.assertTrue(observed["manifest_present"])
        for unsupported in (None, 0, 2, True, "1"):
            with self.assertRaises(ValueError):
                observe_current_image(self.profile, self.state, self.manifest,
                                      {**running, "runtime_owner_protocol": unsupported}, self.raw)
        metadata = self.selected.parent/"manifest.json"
        metadata.write_bytes(self.raw+b" ")
        with self.assertRaises(ValueError):
            observe_current_image(self.profile, self.state, self.manifest, running, self.raw)
        metadata.unlink()
        observed = observe_current_image(self.profile, self.state, self.manifest, running, None)
        self.assertFalse(observed["manifest_present"])
        with self.assertRaises(FileNotFoundError):
            observe_current_image(self.profile, self.state, self.manifest, running, self.raw)


@unittest.skipUnless(sys.platform.startswith("linux"), "requires Linux memfd/pidfd")
class LinuxImageTests(unittest.TestCase):
    def start_image(self, sealed=True):
        import errno
        import fcntl
        temporary = tempfile.TemporaryDirectory()
        self.addCleanup(temporary.cleanup)
        selected = Path(temporary.name)/"runtime"
        data = Path("/proc/self/exe").read_bytes()
        selected.write_bytes(data)
        manifest = {"build": {"profile_format": 1}, "artifact": {
            "filename": selected.name, "size": len(data), "sha256": hashlib.sha256(data).hexdigest()}}
        name = memfd_label(selected).removeprefix("/memfd:")
        try:
            fd = os.memfd_create(name, 0x13)
        except OSError as error:
            if error.errno != errno.EINVAL:
                raise
            fd = os.memfd_create(name, 3)
        self.addCleanup(os.close, fd)
        with os.fdopen(os.dup(fd), "wb") as image:
            image.write(data)
        os.fchmod(fd, 0o500)
        if sealed:
            fcntl.fcntl(fd, fcntl.F_ADD_SEALS, 0x0f)
        environment = dict(os.environ, PYTHONHOME=sys.prefix)
        child = subprocess.Popen([f"/proc/self/fd/{fd}", "-c",
                                  "import sys;print('ready',flush=True);sys.stdin.read(1)"],
                                 pass_fds=(fd,), stdin=subprocess.PIPE, stdout=subprocess.PIPE,
                                 stderr=subprocess.PIPE, env=environment)
        def cleanup():
            if child.poll() is None:
                child.terminate()
            child.communicate(timeout=5)
        self.addCleanup(cleanup)
        import select
        self.assertTrue(select.select([child.stdout], [], [], 5)[0], "native image did not start")
        self.assertEqual(child.stdout.readline(), b"ready\n")
        return selected, manifest, child

    # This is a real sealed native executable observation, not a signature or
    # Runtime service fixture. Service-role checking remains on in real harnesses.
    def test_observes_sealed_kernel_bytes_and_held_process_exit(self):
        selected, manifest, child = self.start_image()
        with LinuxProcessImage(child.pid) as process:
            observed = process.observe(selected, manifest, require_service=False)
            self.assertEqual(observed["loaded_sha256"], manifest["artifact"]["sha256"])
            self.assertEqual(observed["seals"] & 0x0f, 0x0f)
            self.assertEqual(observed["parent_pid"], os.getpid())
            with self.assertRaises(ValueError):
                process.observe(selected, manifest, require_service=False, expected_parent=os.getpid()+1)
            with self.assertRaises(ValueError):
                process.observe(selected, manifest)  # Python is not runtime-serve.
            selected.write_bytes(b"disk replacement")
            self.assertEqual(process.observe(selected, manifest, require_service=False)["loaded_sha256"],
                             manifest["artifact"]["sha256"])
            child.terminate()
            child.wait(timeout=5)
            self.assertTrue(process.exited())
            with self.assertRaises(ProcessLookupError):
                process.observe(selected, manifest, require_service=False)

    # A matching label and digest are insufficient without kernel content seals.
    def test_rejects_unsealed_image(self):
        selected, manifest, child = self.start_image(sealed=False)
        with LinuxProcessImage(child.pid) as process, self.assertRaises(ValueError):
            process.observe(selected, manifest, require_service=False)


if __name__ == "__main__":
    unittest.main()
