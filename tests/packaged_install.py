"""Install the npm tarball in a disposable prefix and check package/entrypoint layout.

Production Runtime installation is covered only by release_install_source_e2e.py
with an independently authenticated raw bundle; this test never calls runtime
install on an unsigned local tarball.
"""
import json
import argparse
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile

sys.stdout.reconfigure(encoding="utf-8", errors="replace")
ROOT = Path(__file__).resolve().parents[1]


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--allow-dirty',action='store_true',help='Allow explicitly marked local test artifacts; CI remains clean-only')
    args = parser.parse_args()
    version = json.loads((ROOT / "packages/mcp/package.json").read_text(encoding="utf-8"))["version"]
    archive = ROOT / f"dist/release/xallor-remote-mcp-{version}.tgz"
    assert archive.is_file(), archive
    root = Path(tempfile.mkdtemp(prefix="xr-package-")).resolve()
    (root / ".owner").write_text("packaged-install", encoding="utf-8")
    clean = True
    try:
        npm = shutil.which("npm.cmd" if os.name == "nt" else "npm")
        env = os.environ.copy()
        subprocess.run([npm, "install", "--prefix", str(root), "--no-audit", "--no-fund", str(archive)],
                       check=True, timeout=180, env=env)
        pkg = root / "node_modules/xallor-remote-mcp"
        metadata = json.loads((pkg / "package.json").read_text(encoding="utf-8"))
        wrapper = pkg / metadata["bin"]["xallor-remote"]
        vendor = pkg / ("vendor/win32-x64" if os.name == "nt" else "vendor/linux-x64")
        binary = vendor / ("xallor-remote.exe" if os.name == "nt" else "xallor-remote")

        def cli(*args):
            return subprocess.run(["node", str(wrapper), *args], env=os.environ.copy(), check=True,
                                  capture_output=True, encoding="utf-8", timeout=30).stdout

        actual = json.loads(cli("version", "--json"))
        manifest = json.loads((vendor / "manifest.json").read_text(encoding="utf-8"))
        assert actual["version"] == version and actual["revision"] == manifest["build"]["revision"]
        assert actual["dirty"] == manifest["build"]["dirty"]
        assert not actual["dirty"] or args.allow_dirty, 'Use --allow-dirty only for local verification of uncommitted artifacts'
        assert wrapper.is_file() and binary.is_file() and (vendor / "manifest.json").is_file()
        help_output = subprocess.run(["node", str(wrapper), "--help"], capture_output=True,
                                     encoding="utf-8", errors="replace", check=True, timeout=30)
        assert help_output.stdout or help_output.stderr
        output = ROOT / "dist/verification/packaged-install.json"
        output.parent.mkdir(parents=True, exist_ok=True)
        output.write_text(json.dumps({"status": "pass", "version": version,
                                      "revision": actual["revision"], "platform": sys.platform,
                                      "dirty": actual["dirty"], "runtime_install": "not_run",
                                      "scope": "npm package layout and entrypoints"}), encoding="utf-8")
        print("Packaged CLI layout and entrypoints passed; Runtime install intentionally not run", flush=True)
    finally:
        if clean:
            assert root.parent == Path(tempfile.gettempdir()).resolve() and root.name.startswith("xr-package-")
            assert (root / ".owner").read_text(encoding="utf-8") == "packaged-install"
            # npm may create executable symlinks. rmtree does not follow them.
            shutil.rmtree(root)
        else:
            raise RuntimeError(f"Test profile retained for process cleanup: {root}")


if __name__ == "__main__":
    main()
