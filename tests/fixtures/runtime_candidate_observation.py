"""Discover only children of a retained worker, then bind the actual image/role."""
import os
from pathlib import Path
import sys

from runtime_image_observation import memfd_label, open_process_image


def find_candidate(worker, selected, manifest):
    if worker.exited():raise ProcessLookupError("Worker exited before candidate observation")
    if sys.platform=="win32":
        from runtime_process_windows import child_process_ids
        pids=child_process_ids(worker.pid)
    elif sys.platform.startswith("linux"):
        pids=[]
        label=memfd_label(selected)
        for process in Path("/proc").iterdir():
            if not process.name.isdecimal():continue
            try:
                fields=(process/"stat").read_text().rsplit(")",1)[1].split()
                if int(fields[1])!=worker.pid:continue
                if (process/"exe").readlink().as_posix() not in (label,label+" (deleted)"):continue
                pids.append(int(process.name))
            except (FileNotFoundError,ProcessLookupError,PermissionError):continue
    else:
        raise RuntimeError("Native candidate observation requires Windows or Linux")
    for pid in pids:
        reference=None
        try:
            reference=open_process_image(pid)
            if sys.platform=="win32":
                if reference.parent_pid()!=worker.pid:continue
                # Path is a discovery filter only. The retained file mapping,
                # digest, exact entry and parent are checked again below.
                if os.path.normcase(str(Path(reference.image_path()).resolve()))!=os.path.normcase(str(Path(selected).resolve())):continue
            observed=reference.observe(selected,manifest,entry="runtime-serve",expected_parent=worker.pid)
            if worker.exited():raise RuntimeError("Retained worker exited during candidate binding")
            result=reference;reference=None
            return result,observed
        except ProcessLookupError:
            continue
        except OSError as error:
            if sys.platform=="win32" and getattr(error,"winerror",None)==87:continue
            raise
        finally:
            if reference is not None:reference.close()
    if worker.exited():raise ProcessLookupError("Worker exited during candidate discovery")
    return None,None
