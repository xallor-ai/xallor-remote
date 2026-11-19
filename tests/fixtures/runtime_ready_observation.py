"""Bind a test-held image reference to the existing ready owner IPC reply."""
import json
import os
import re
import subprocess


def query_owner(observer, profile, env):
    flags = {"creationflags": subprocess.CREATE_NO_WINDOW} if os.name == "nt" else {}
    result = subprocess.run([str(observer), "--profile", str(profile)], env=env,
                            capture_output=True, encoding="utf-8", errors="replace", timeout=75, **flags)
    if result.returncode != 0:
        raise RuntimeError("Read-only owner observation failed: " + result.stderr.strip())
    return json.loads(result.stdout)


def validate_ready(observed, installed, manifest, running):
    runtime = observed.get("runtime", {})
    if (type(observed.get("peer_pid")) is not int or observed["peer_pid"] != running["pid"]
            or type(runtime.get("pid")) is not int or runtime["pid"] != running["pid"]
            or runtime.get("state") != "ready" or runtime.get("code")):
        raise ValueError("Owner readiness is not bound to the observed native peer")
    for field in ("version", "revision", "protocol", "profile_format"):
        if runtime.get(field) != manifest["build"][field] or type(runtime.get(field)) is not type(manifest["build"][field]):
            raise ValueError("Owner ready build differs from authenticated manifest")
    if runtime.get("sha256") != manifest["artifact"]["sha256"]:
        raise ValueError("Owner ready digest differs from authenticated image")
    for field, state_field in (("activation_id", "current_activation"), ("launch_id", "current_launch_id")):
        value = runtime.get(field)
        if not isinstance(value, str) or not re.fullmatch("[a-f0-9]{32}", value) or value != installed.get(state_field):
            raise ValueError("Owner ready activation/launch differs from selection")
    return dict(runtime)
