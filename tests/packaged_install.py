"""Install the actual npm tarball in a disposable prefix and exercise its entrypoints."""
import json
import argparse
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile
import time

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
    profile = root / "profile"
    env = dict(os.environ, XALLOR_REMOTE_DATA_DIR=str(profile),
               XALLOR_REMOTE_RELAY_URL="ws://127.0.0.1:1/remote")
    env.pop("XALLOR_REMOTE_BIN", None)
    binary = None
    clean = True
    try:
        npm = shutil.which("npm.cmd" if os.name == "nt" else "npm")
        subprocess.run([npm, "install", "--prefix", str(root), "--no-audit", "--no-fund", str(archive)],
                       check=True, timeout=180, env=env)
        pkg = root / "node_modules/xallor-remote-mcp"
        metadata = json.loads((pkg / "package.json").read_text(encoding="utf-8"))
        wrapper = pkg / metadata["bin"]["xallor-remote"]
        vendor = pkg / ("vendor/win32-x64" if os.name == "nt" else "vendor/linux-x64")
        binary = vendor / ("xallor-remote.exe" if os.name == "nt" else "xallor-remote")

        def cli(*args):
            return subprocess.run(["node", str(wrapper), *args], env=env, check=True,
                                  capture_output=True, encoding="utf-8", timeout=30).stdout

        actual = json.loads(cli("version", "--json"))
        manifest = json.loads((vendor / "manifest.json").read_text(encoding="utf-8"))
        assert actual["version"] == version and actual["revision"] == manifest["build"]["revision"]
        assert actual["dirty"] == manifest["build"]["dirty"]
        assert not actual["dirty"] or args.allow_dirty, 'Use --allow-dirty only for local verification of uncommitted artifacts'
        cli("runtime", "install", "--bundle", str(vendor))
        status = json.loads(cli("runtime", "status"))
        assert status["running"]["sha256"] == manifest["artifact"]["sha256"]
        smoke = dict(env, XALLOR_TEST_CLI=str(binary), XALLOR_TEST_MCP_ENTRY=str(pkg / "dist/index.js"))
        subprocess.run(["node", str(ROOT / "tests/helpers/mcp-release-smoke.mjs")],
                       env=smoke, check=True, timeout=60)
        output = ROOT / "dist/verification/packaged-install.json"
        output.parent.mkdir(parents=True, exist_ok=True)
        output.write_text(json.dumps({"status": "pass", "version": version,
                                      "revision": actual["revision"], "platform": sys.platform,
                                      "dirty": actual["dirty"]}), encoding="utf-8")
        print("Packaged CLI, verified install and MCP reconnect passed", flush=True)
    finally:
        if binary is not None and binary.exists():
            subprocess.run([str(binary), "stop"], env=env, capture_output=True, timeout=20)
            deadline = time.monotonic() + 10
            while True:
                check = subprocess.run([str(binary), "runtime", "status"], env=env,
                                       capture_output=True, encoding="utf-8", errors="replace", timeout=20)
                if check.returncode != 0:
                    clean = False
                    break
                if json.loads(check.stdout)["running"] is None:
                    break
                if time.monotonic() > deadline:
                    clean = False
                    break
                time.sleep(.1)
        if clean:
            assert root.parent == Path(tempfile.gettempdir()).resolve() and root.name.startswith("xr-package-")
            assert (root / ".owner").read_text(encoding="utf-8") == "packaged-install"
            # npm may create executable symlinks. rmtree does not follow them.
            shutil.rmtree(root)
        else:
            raise RuntimeError(f"Test profile retained for process cleanup: {root}")


if __name__ == "__main__":
    main()
