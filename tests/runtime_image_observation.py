"""Image observations for acceptance scripts; never an execution authority."""
import hashlib
import os
from pathlib import Path
import re
import stat
import sys


MAX_BINARY = 256 * 1024 * 1024
REQUIRED_SEALS = 0x0F


def memfd_label(selected):
    absolute = os.path.abspath(os.fspath(selected))
    return "/memfd:xr-runtime-"+hashlib.sha256(os.fsencode(absolute)).hexdigest()


def reported_location_matches(observed, selected, platform=sys.platform):
    """Compare the Runtime's reported location with its selected activation."""
    if platform.startswith("linux"):
        return observed == memfd_label(selected)
    return os.path.normcase(str(Path(observed).resolve())) == os.path.normcase(str(Path(selected).resolve()))


def selected_activation(profile, installed, manifest, role="current"):
    if role not in ("current", "pending"):
        raise ValueError("Unsupported activation role")
    activation_id = installed.get(role+"_activation")
    if not isinstance(activation_id, str) or not re.fullmatch("[a-f0-9]{32}", activation_id):
        raise ValueError("Missing or invalid activation ID")
    if installed.get(role) != manifest:
        raise ValueError("Selected manifest differs from expected signed manifest")
    filename = manifest["artifact"]["filename"]
    if not isinstance(filename, str) or "/" in filename or "\\" in filename or filename in ("", ".", ".."):
        raise ValueError("Invalid artifact filename")
    root = Path(profile).resolve()/"runtime"/"activations"
    directory = root/activation_id
    if directory.resolve() != directory or directory.is_symlink():
        raise ValueError("Activation directory is redirected")
    return directory/filename


def hash_open_image(image, artifact):
    info = os.fstat(image.fileno())
    size = artifact["size"]
    if type(size) is not int or not 0 < size <= MAX_BINARY:
        raise ValueError("Invalid expected image size")
    if not stat.S_ISREG(info.st_mode) or info.st_size != size:
        raise ValueError("Image type/size differs from manifest")
    digest = hashlib.sha256()
    total = 0
    while chunk := image.read(128*1024):
        total += len(chunk)
        if total > size:
            raise ValueError("Image grew during observation")
        digest.update(chunk)
    actual = digest.hexdigest()
    if total != size or actual != artifact["sha256"]:
        raise ValueError("Image digest differs from manifest")
    return actual


class LinuxProcessImage:
    """A held pidfd prevents treating exit/PID reuse during a check as success."""
    def __init__(self, pid):
        if not sys.platform.startswith("linux") or not hasattr(os, "pidfd_open"):
            raise RuntimeError("Kernel process observation requires Linux pidfd support")
        if type(pid) is not int or pid <= 0:
            raise ValueError("Invalid Runtime PID")
        self.pid = pid
        self.fd = os.pidfd_open(pid, 0)

    def close(self):
        if self.fd is not None:
            os.close(self.fd)
            self.fd = None

    def __enter__(self):
        return self

    def __exit__(self, *_args):
        self.close()

    def exited(self):
        import select
        if self.fd is None:
            raise ValueError("Process observation already closed")
        poll = select.poll()
        poll.register(self.fd, select.POLLIN)
        events = poll.poll(0)
        if any(flags & (select.POLLERR | select.POLLNVAL) for _, flags in events):
            raise RuntimeError("Process observation failed")
        return any(flags & (select.POLLIN | select.POLLHUP) for _, flags in events)

    def observe(self, selected, manifest, require_service=True, expected_parent=None, entry="runtime-serve"):
        import fcntl
        if self.exited():
            raise ProcessLookupError("Runtime exited before image observation")
        process = Path("/proc")/str(self.pid)
        label = memfd_label(selected)
        loaded = os.readlink(process/"exe")
        if loaded not in (label, label+" (deleted)"):
            raise ValueError("Kernel image is not the selected sealed activation")
        if require_service:
            if entry not in ("runtime-serve", "runtime-transaction"):
                raise ValueError("Unsupported Runtime observation entry")
            # argv0 is the original activation, even when exec uses a memfd.
            # No environment/credentials need to be read to scope this process.
            with (process/"cmdline").open("rb") as arguments:
                command = arguments.read(65537)
            expected = [os.fsencode(os.path.abspath(selected)), b"--require-profile-format",
                        str(manifest["build"]["profile_format"]).encode(), entry.encode(), b""]
            if len(command) > 65536 or command.split(b"\0") != expected:
                raise ValueError("Process is not the scoped Runtime entry")
        with (process/"exe").open("rb") as image:
            seals = fcntl.fcntl(image.fileno(), fcntl.F_GET_SEALS)
            if seals & REQUIRED_SEALS != REQUIRED_SEALS:
                raise ValueError("Loaded execution image is not sealed")
            digest = hash_open_image(image, manifest["artifact"])
        process_fields = (process/"stat").read_text().rsplit(")", 1)[1].split()
        start_ticks = process_fields[19]
        parent_pid = int(process_fields[1])
        if expected_parent is not None and parent_pid != expected_parent:
            raise ValueError("Candidate is not a child of the scoped installer")
        if self.exited():
            raise ProcessLookupError("Runtime exited during image observation")
        result = {"kernel_path": loaded, "loaded_sha256": digest, "seals": seals,
                  "start_ticks": start_ticks, "parent_pid": parent_pid, "pidfd_observed": True}
        if require_service:
            result["entry"] = entry
        return result


def open_process_image(pid):
    if sys.platform.startswith("linux"):
        return LinuxProcessImage(pid)
    if sys.platform == "win32":
        from runtime_image_windows import WindowsProcessImage
        return WindowsProcessImage(pid)
    raise RuntimeError("Native image observation requires Windows or Linux")


def observe_current_image(profile, installed, manifest, running, original_manifest, process=None):
    """Source verification remains the caller's job. This observes selected and
    loaded bytes. None means the explicit missing-manifest negative test only."""
    if installed.get("pending") or installed.get("pending_activation"):
        raise ValueError("Pending state cannot establish selected-image acceptance")
    if (type(running.get("pid")) is not int or running["pid"] <= 0 or
            type(running.get("runtime_owner_protocol")) is not int or running["runtime_owner_protocol"] != 1):
        raise ValueError("Missing owner process identity/capability")
    selected = selected_activation(profile, installed, manifest)
    if not reported_location_matches(running["path"], selected):
        raise ValueError("Reported image does not match selection")
    if running.get("sha256") != manifest["artifact"]["sha256"]:
        raise ValueError("Reported loaded-image digest differs from signed manifest")
    if selected.is_symlink():
        raise ValueError("Activation binary is redirected")
    with selected.open("rb") as image:
        disk_digest = hash_open_image(image, manifest["artifact"])
    metadata = selected.parent/"manifest.json"
    if original_manifest is None:
        if os.path.lexists(metadata):
            raise ValueError("Missing-manifest negative case unexpectedly has metadata")
    else:
        if metadata.is_symlink():
            raise ValueError("Original manifest is redirected")
        with metadata.open("rb") as manifest_file:
            info = os.fstat(manifest_file.fileno())
            retained_manifest = manifest_file.read(65537)
        if (not stat.S_ISREG(info.st_mode) or info.st_size > 65536 or
                len(retained_manifest) > 65536 or retained_manifest != original_manifest):
            raise ValueError("Original signed manifest bytes were not retained")
    result = {"activation_path": str(selected), "retained_sha256": disk_digest,
              "manifest_present": original_manifest is not None, "pidfd_observed": False}
    if process is None:
        with open_process_image(running["pid"]) as observed:
            result.update(observed.observe(selected, manifest))
    else:
        if process.pid != running["pid"]:
            raise ValueError("Held process differs from Runtime observation")
        result.update(process.observe(selected, manifest))
    return result
