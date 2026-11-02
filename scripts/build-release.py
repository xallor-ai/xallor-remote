"""Build offline Runtime bundles with embedded identity and integrity manifests."""
import argparse
import datetime
import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys

sys.stdout.reconfigure(encoding="utf-8", errors="replace")
ROOT = Path(__file__).resolve().parents[1]


def git(*args):
    return subprocess.check_output(["git", *args], cwd=ROOT, text=True, encoding="utf-8").strip()


def build(output, platform, go, allow_dirty=False):
    version = (ROOT / "internal/buildinfo/version.txt").read_text(encoding="utf-8").strip()
    package = json.loads((ROOT / "packages/mcp/package.json").read_text(encoding="utf-8"))
    if package["version"] != version:
        raise RuntimeError("package version differs from internal/buildinfo/version.txt")
    ref = os.environ.get("GITHUB_REF", "")
    if ref.startswith("refs/tags/v") and ref.removeprefix("refs/tags/v") != version:
        raise RuntimeError("release tag does not match package version")
    dirty = bool(git("status", "--porcelain", "--untracked-files=normal"))
    if dirty and not allow_dirty:
        raise RuntimeError("release source is dirty; commit first or use --allow-dirty for local tests")
    revision = git("rev-parse", "HEAD")
    epoch = int(os.environ.get("SOURCE_DATE_EPOCH", str(int(datetime.datetime.now().timestamp()))))
    built_at = datetime.datetime.fromtimestamp(epoch, datetime.timezone.utc).isoformat().replace("+00:00", "Z")
    goos, arch = platform.split("/")
    if platform not in ("windows/amd64", "linux/amd64"):
        raise RuntimeError("unsupported release platform")
    target = Path(output).resolve() / f"{goos}-{arch}"
    target.mkdir(parents=True, exist_ok=True)
    name = "xallor-remote.exe" if goos == "windows" else "xallor-remote"
    destination = target / name
    prefix = "github.com/xallor-ai/xallor-remote/internal/buildinfo."
    flags = f"-X {prefix}Revision={revision} -X {prefix}BuiltAt={built_at} -X {prefix}Modified={str(dirty).lower()}"
    env = dict(os.environ, GOOS=goos, GOARCH=arch, CGO_ENABLED="0")
    subprocess.run([go, "build", "-trimpath", "-buildvcs=false", "-ldflags", flags,
                    "-o", str(destination), "./cmd/xallor-remote"], cwd=ROOT, env=env, check=True)
    digest = hashlib.sha256(destination.read_bytes()).hexdigest()
    manifest = {
        "schema": 1,
        "build": {"version": version, "revision": revision, "built_at": built_at,
                  "dirty": dirty, "protocol": 1, "os": goos, "arch": arch},
        "artifact": {"filename": name, "sha256": digest, "size": destination.stat().st_size},
    }
    (target / "manifest.json").write_text(json.dumps(manifest, indent=2) + "\n", encoding="utf-8")
    archive = shutil.make_archive(str(Path(output).resolve() / f"xallor-remote-{version}-{goos}-{arch}"),
                                  "zip", root_dir=target)
    checksum = hashlib.sha256(Path(archive).read_bytes()).hexdigest()
    Path(archive + ".sha256").write_text(checksum + "  " + Path(archive).name + "\n", encoding="utf-8")
    print(json.dumps({"bundle": str(target), "sha256": digest, "revision": revision, "dirty": dirty}), flush=True)
    return target


def main():
    p = argparse.ArgumentParser()
    p.add_argument("--output", default=str(ROOT / "dist/release"))
    p.add_argument("--platform", action="append", choices=["windows/amd64", "linux/amd64"])
    p.add_argument("--go", default="go")
    p.add_argument("--allow-dirty", action="store_true")
    args = p.parse_args()
    for platform in args.platform or ["windows/amd64", "linux/amd64"]:
        build(args.output, platform, args.go, args.allow_dirty)


if __name__ == "__main__":
    main()
