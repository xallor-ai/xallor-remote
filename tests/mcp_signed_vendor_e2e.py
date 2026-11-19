"""Real signed-vendor wrapper acceptance; isolated package, no Runtime service.
The launcher is the current local build, not a claim that a release tgz contains it.
"""
import argparse
import hashlib
import json
import os
import re
from pathlib import Path
import shutil
import signal
import subprocess
import sys
import tempfile

ROOT = Path(__file__).resolve().parents[1]


def run(command, env, cwd=None, timeout=180):
    options = ({"creationflags": subprocess.CREATE_NO_WINDOW} if os.name == "nt"
               else {"start_new_session": True})
    with subprocess.Popen(command, env=env, cwd=cwd, stdout=subprocess.PIPE,
                          stderr=subprocess.PIPE, text=True, encoding="utf-8",
                          errors="replace", **options) as process:
        try:
            stdout, stderr = process.communicate(timeout=timeout)
        except subprocess.TimeoutExpired:
            if os.name == "nt" and process.poll() is None:
                subprocess.run(["taskkill", "/PID", str(process.pid), "/T", "/F"],
                               capture_output=True, **options)
            elif os.name != "nt":
                try:
                    os.killpg(process.pid, signal.SIGKILL)
                except ProcessLookupError:
                    pass
            process.kill()
            process.communicate()
            raise
        return subprocess.CompletedProcess(command, process.returncode, stdout, stderr)


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--signed-bundle", required=True)
    parser.add_argument("--package-build", required=True)
    parser.add_argument("--foreign-manifest", required=True, help="Authentic opposite-platform manifest from the same signed release")
    parser.add_argument("--go", default="go")
    parser.add_argument("--fixture-binary", help="Native source-candidate fixture cross-built from this checkout; calibrated before use")
    parser.add_argument("--output", required=True)
    args = parser.parse_args()
    source = Path(args.signed_bundle).resolve()
    package_build = Path(args.package_build).resolve()
    node = shutil.which("node")
    gh = shutil.which("gh")
    if not node or not gh:
        raise RuntimeError("Trusted system node and gh are required")
    manifest_raw = (source / "manifest.json").read_bytes()
    manifest = json.loads(manifest_raw)
    name = "xallor-remote.exe" if os.name == "nt" else "xallor-remote"
    platform = "win32-x64" if os.name == "nt" else "linux-x64"
    expected_os = "windows" if os.name == "nt" else "linux"
    if (manifest["artifact"]["filename"] != name or manifest["build"]["os"] != expected_os
            or manifest["build"]["arch"] != "amd64"):
        raise RuntimeError("This acceptance harness requires a native amd64 signed bundle")
    build = json.loads(package_build.read_text(encoding="utf-8"))
    if build["version"] != manifest["build"]["version"]:
        raise RuntimeError("Package and candidate versions differ")
    results = []
    with tempfile.TemporaryDirectory(prefix="xr-signed-vendor-") as temporary:
        work = Path(temporary).resolve()
        pkg = work / "package"
        shutil.copytree(ROOT / "packages/mcp/dist", pkg / "dist")
        shutil.copy2(ROOT / "packages/mcp/package.json", pkg / "package.json")
        # Install-time trusted native binding; no candidate-side library lookup.
        modules = ROOT / "packages/mcp/node_modules"
        shutil.copytree(modules / "koffi", pkg / "node_modules/koffi")
        native_binding = "@koromix/koffi-" + platform
        shutil.copytree(modules / native_binding, pkg / "node_modules" / native_binding)
        shutil.copy2(package_build, pkg / "build.json")
        vendor = pkg / "vendor" / platform
        vendor.mkdir(parents=True)
        binary = vendor / name
        metadata = vendor / "manifest.json"
        shutil.copy2(source / name, binary)
        metadata.write_bytes(manifest_raw)
        profile = work / "profile"
        bootstrap_root = work / "bootstrap-copies"
        bootstrap_root.mkdir()
        marker = work / "candidate-executed"
        env = os.environ.copy()
        for key in list(env):
            if key.upper().startswith("XALLOR_") or key.lower() == "path":
                env.pop(key)
        env.update(PATH=str(Path(gh).parent), XALLOR_REMOTE_DATA_DIR=str(profile),
                   XALLOR_REMOTE_RELAY_URL="ws://127.0.0.1:1/remote",
                   TMPDIR=str(bootstrap_root), TMP=str(bootstrap_root), TEMP=str(bootstrap_root),
                   XALLOR_TEST_CANDIDATE_MARKER=str(marker))
        wrapper = pkg / "dist/runtime-bin.js"

        # Positive launch tests real signatures, real selection and real executable identity.
        result = run([node, str(wrapper), "version", "--json"], env)
        if result.returncode:
            raise AssertionError(result.stderr)
        actual = json.loads(result.stdout)
        for field in ("version", "revision", "profile_format", "protocol", "os", "arch"):
            assert actual[field] == manifest["build"][field], (field, actual)
        if sys.platform.startswith("linux"):
            # The short-lived signed version command reports its kernel image.
            # Its original temporary path is deliberately not faked as loaded.
            assert re.fullmatch(r"/memfd:xr-runtime-[a-f0-9]{64}", actual["path"]), actual
        else:
            executed = Path(actual["path"]).resolve()
            assert executed != binary.resolve(), "Wrapper executed mutable vendor path"
            assert executed.name == name and executed.parent.parent == bootstrap_root.resolve(), actual
            assert executed.parent.name.startswith("xr-bootstrap-"), actual
            assert not executed.parent.exists(), "Closed bootstrap snapshot was not cleaned"
        assert not list(bootstrap_root.iterdir()), "Bootstrap capture left untracked copies"
        assert actual["sha256"] == manifest["artifact"]["sha256"], "Reported loaded-image digest differs; an old signed binary cannot be used as new-launcher acceptance"
        assert not profile.exists()
        results.append({"case": "signed_vendor_launch", "passed": True, "build": actual})

        # A separately signed manifest from the same release must not describe
        # this platform's binary. First prove the foreign subject is authentic.
        foreign = Path(args.foreign_manifest).resolve()
        other = json.loads(foreign.read_bytes())
        assert other["build"]["os"] != expected_os
        assert other["build"]["revision"] == manifest["build"]["revision"]
        assert other["build"]["version"] == manifest["build"]["version"]
        proof = run([gh, "attestation", "verify", str(foreign), "--repo", "xallor-ai/xallor-remote",
                     "--signer-workflow", "xallor-ai/xallor-remote/.github/workflows/release.yml",
                     "--cert-oidc-issuer", "https://token.actions.githubusercontent.com",
                     "--predicate-type", "https://slsa.dev/provenance/v1", "--deny-self-hosted-runners",
                     "--source-ref", "refs/tags/v"+manifest["build"]["version"],
                     "--source-digest", manifest["build"]["revision"]], env)
        assert proof.returncode == 0, proof.stderr
        metadata.write_bytes(foreign.read_bytes())
        result = run([node, str(wrapper), "version", "--json"], env)
        assert result.returncode != 0 and not result.stdout.strip() and not profile.exists(), result
        results.append({"case": "individually_signed_foreign_manifest_refused", "passed": True})

        # Same binary, modified original metadata: no successful version result.
        metadata.write_bytes(manifest_raw + b"\n ")
        result = run([node, str(wrapper), "version", "--json"], env)
        assert result.returncode != 0 and not result.stdout.strip(), result
        assert not profile.exists()
        results.append({"case": "altered_manifest_refused", "passed": True})

        # A real executable leaves a marker on ANY execution. Calibrate it first.
        fixture = work / name
        if args.fixture_binary:
            shutil.copy2(Path(args.fixture_binary).resolve(), fixture)
        else:
            compiled = run([args.go, "build", "-o", str(fixture),
                            "./tests/fixtures/source-candidate"], os.environ.copy(), cwd=ROOT)
            assert compiled.returncode == 0, compiled.stderr
        calibrated = run([str(fixture)], env)
        assert calibrated.returncode == 0 and marker.is_file()
        marker.unlink()
        shutil.copy2(fixture, binary)
        # Keep the authentic manifest: rejection must now cover the binary,
        # not merely the already-tested altered-manifest branch.
        metadata.write_bytes(manifest_raw)
        result = run([node, str(wrapper), "version", "--json"], env)
        assert result.returncode != 0 and not marker.exists() and not profile.exists(), result
        results.append({"case": "unsigned_binary_with_authentic_manifest_refused", "passed": True})
        altered = json.loads(manifest_raw)
        altered["artifact"]["size"] = binary.stat().st_size
        altered["artifact"]["sha256"] = hashlib.sha256(binary.read_bytes()).hexdigest()
        metadata.write_text(json.dumps(altered), encoding="utf-8")
        result = run([node, str(wrapper), "version", "--json"], env)
        assert result.returncode != 0 and not marker.exists() and not profile.exists(), result
        results.append({"case": "rehashed_executable_refused_before_execution", "passed": True})
        assert not list(bootstrap_root.iterdir()), "Refused bootstrap left private copies"

    output = Path(args.output).resolve()
    output.parent.mkdir(parents=True, exist_ok=True)
    output.write_text(json.dumps({
        "scope": "local launcher with real signed native vendor; version command only",
        "platform": sys.platform, "source_revision": manifest["build"]["revision"],
        "launcher_sha256": hashlib.sha256((ROOT / "packages/mcp/dist/runtime-bin.js").read_bytes()).hexdigest(),
        "source_trust_sha256": hashlib.sha256((ROOT / "packages/mcp/dist/source-trust.js").read_bytes()).hexdigest(),
        "bootstrap_modules_sha256": {name: hashlib.sha256((ROOT / "packages/mcp/dist" / name).read_bytes()).hexdigest()
                                     for name in ("bootstrap-launcher.js", "bootstrap-snapshot.js", "bootstrap-process.js",
                                                  "bootstrap-image.js", "bootstrap-image-windows.js", "bootstrap-image-linux.js")},
        "dependency_lock_sha256": hashlib.sha256((ROOT / "packages/mcp/package-lock.json").read_bytes()).hexdigest(),
        "results": results, "cleanup_complete": True,
    }, indent=2), encoding="utf-8")
    print(json.dumps(results, ensure_ascii=False))


if __name__ == "__main__":
    sys.stdout.reconfigure(encoding="utf-8", errors="replace")
    main()

