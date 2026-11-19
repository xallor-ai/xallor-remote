"""Explicit native owner mechanics fixture, never release/signature evidence."""
import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess

ROOT = Path(__file__).resolve().parents[2]


def build_owner_mechanics(owned_root, go, prebuilt=None):
    """Build Go's _test-only dispatch. Production binaries have no such mode."""
    root = Path(owned_root).resolve()
    source, tools = root/"mechanics-bundle", root/"mechanics-tools"
    source.mkdir()
    tools.mkdir()
    suffix = ".exe" if os.name == "nt" else ""
    binary = source/("xallor-remote"+suffix)
    quiet = {"creationflags": subprocess.CREATE_NO_WINDOW} if os.name == "nt" else {}
    if prebuilt is None:
        subprocess.run([go, "test", "-c", "-o", str(binary), "./internal/cli"],
                       cwd=ROOT, check=True, timeout=180, **quiet)
    else:
        # Explicit trusted test input for offline cross-platform execution.
        # Its source provenance is not inferred from self-reported metadata.
        shutil.copy2(Path(prebuilt).resolve(), binary)
        binary.chmod(0o700)
    verifier = tools/("gh"+suffix)
    shutil.copy2(binary, verifier)
    env = {"XR_TEST_NATIVE_OWNER_FLOW": "1",
           "PATH": str(tools)+os.pathsep+os.environ.get("PATH", "")}
    probe_env = {key: value for key, value in os.environ.items()
                 if not key.upper().startswith(("XALLOR_", "XR_TEST_")) and key.lower() != "path"}
    probe_env.update(env)
    observed = subprocess.run([str(binary), "version", "--json"], env=probe_env,
                              capture_output=True, text=True, encoding="utf-8",
                              errors="replace", check=True, timeout=30, **quiet)
    build = json.loads(observed.stdout)
    if build["revision"] != "a"*40 or build["dirty"] or build.get("runtime_owner_protocol") != 1:
        raise RuntimeError("Unexpected native test fixture identity")
    digest = hashlib.sha256(binary.read_bytes()).hexdigest()
    if build["sha256"] != digest:
        raise RuntimeError("Native fixture loaded digest differs")
    manifest = {"schema": 1,
                "build": {key: build[key] for key in ("version", "revision", "built_at", "dirty",
                                                    "protocol", "profile_format", "os", "arch") if key in build},
                "artifact": {"filename": binary.name, "size": binary.stat().st_size, "sha256": digest}}
    (source/"manifest.json").write_text(json.dumps(manifest), encoding="utf-8")
    evidence = {"mode": "native-owner-mechanics", "cryptographic_acceptance": False,
                "source": "go test -c ./internal/cli" if prebuilt is None else "supplied precompiled native _test fixture; source provenance not verified",
                "fixture_sha256": digest, "verifier": "controlled _test executable"}
    (root/"mechanics-evidence.json").write_text(json.dumps(evidence, indent=2), encoding="utf-8")
    return binary, env, evidence
