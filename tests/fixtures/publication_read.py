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
            code = process.poll() if process is not None else None
            if code is not None:
                # The caller owns process cleanup. Inherited pipes can remain
                # open after root exit, so collecting output here could hang.
                raise AssertionError(f"fixture command exited before startup: exit code {code}") from error
            if time.monotonic() >= deadline:
                raise TimeoutError(f"fixture publication did not become readable: {path.name}") from error
        time.sleep(.02)
