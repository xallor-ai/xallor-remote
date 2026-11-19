"""Only read the original admitted lifecycle operation; never retry its action."""
import json
import os
from pathlib import Path
import re
import subprocess


def notice_id(path):
    with Path(path).open("rb") as stream:
        raw = stream.read(16385)
    if len(raw) > 16384:
        raise ValueError("Lifecycle notice exceeds bound")
    ids = set(re.findall(r"版本事务：([a-f0-9]{32})；", raw.decode("utf-8", "replace")))
    if len(ids) > 1:
        raise ValueError("More than one lifecycle submission was observed")
    return next(iter(ids)) if ids else None


def query_operation(observer, profile, operation_id, bundle, env, journal=False):
    command = [str(observer), "--profile", str(profile), "--operation", operation_id, "--bundle", str(Path(bundle).resolve())]
    if journal:
        command.append("--journal")
    quiet = {"creationflags": subprocess.CREATE_NO_WINDOW} if os.name == "nt" else {}
    result = subprocess.run(command, env=env, capture_output=True, encoding="utf-8", errors="replace", timeout=8, **quiet)
    if result.returncode != 0:
        raise RuntimeError("Lifecycle observation unavailable: " + result.stderr.strip())
    return json.loads(result.stdout)


def worker_identity(observed, baseline):
    worker = observed.get("observer", {})
    peer = observed.get("peer_pid")
    if (observed.get("source") != "lifecycle_ipc" or type(peer) is not int or peer <= 0
            or worker.get("pid") != peer or worker.get("runtime_owner_protocol") != 1):
        raise ValueError("Lifecycle observer is not the native worker peer")
    for key, expected in baseline["build"].items():
        if worker.get(key) != expected:
            raise ValueError("Lifecycle worker is not the authenticated baseline build")
    if worker.get("sha256") != baseline["artifact"]["sha256"]:
        raise ValueError("Lifecycle worker digest differs from authenticated baseline")
    return peer
