"""Bounded read-only observation of atomically published fixture files."""
import errno
import os
import time


def wait_text(path, process=None):
    deadline = time.monotonic() + 15
    while True:
        try:
            return path.read_text(encoding="utf-8")
        except OSError as error:
            # Python's CRT file open may expose only EACCES, without winerror.
            # Limit that case to Windows; retain all other filesystem failures.
            transient = isinstance(error, FileNotFoundError) or (os.name == "nt" and (
                getattr(error, "winerror", None) in (5, 32, 33) or
                isinstance(error, PermissionError) and error.errno == errno.EACCES))
            if not transient:
                raise
            if process is not None and process.poll() is not None:
                raise AssertionError(f"fixture command exited before startup: {process.communicate()!r}") from error
            if time.monotonic() >= deadline:
                raise TimeoutError(f"fixture publication did not become readable: {path.name}") from error
        time.sleep(.02)
