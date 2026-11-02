"""Real browser -> GUI bridge -> isolated Runtime/Relay, never the user's service."""
import argparse
import json
import os
from pathlib import Path
import socket
import subprocess
import sys
import tempfile
import time
import urllib.request
from fixtures.runtime_cluster import Cluster

sys.stdout.reconfigure(encoding="utf-8", errors="replace")
repo = Path(__file__).resolve().parents[1]
parser = argparse.ArgumentParser()
parser.add_argument("--binary", required=True)
parser.add_argument("--native", action="store_true")
args = parser.parse_args()
evidence = Path(os.environ.get("XR_GUI_EVIDENCE") or tempfile.mkdtemp(prefix="xr-gui-evidence-"))
evidence.mkdir(parents=True, exist_ok=True)
cluster = Cluster(args.binary)
server = None
log = (evidence / "vite.log").open("wb")
try:
    cluster.start()
    controller = cluster.add("controller")
    target = cluster.add("target")
    for name, other_name, other_id in [("controller", "target", target), ("target", "controller", controller)]:
        cluster.cli(other_name, "grant", "issue")
        # CLI's human output is not a secret API. Read the owned fixture's grant file.
        grant = cluster.grant(other_name)
        cluster.cli(name, "peer", "add", "--id", other_id, "--grant", grant)
    with socket.socket() as sock:
        sock.bind(("127.0.0.1", 0))
        port = sock.getsockname()[1]
    env = dict(cluster.env("controller"), XALLOR_REMOTE_BIN=str(cluster.binary),
               XR_GUI_URL=f"http://127.0.0.1:{port}", XR_GUI_TARGET=target,
               XR_GUI_CONTROLLER=controller, XR_GUI_ROOT=str(cluster.root), XR_GUI_EVIDENCE=str(evidence))
    gui = repo / "apps" / "gui"
    if args.native:
        subprocess.run(["cargo", "test", "--locked", "--manifest-path", "src-tauri/Cargo.toml", "--", "--ignored"],
                       cwd=gui, env=env, check=True, timeout=600)
    server = subprocess.Popen(["node", "node_modules/vite/bin/vite.js", "--port", str(port)], cwd=gui,
                              env=env, stdout=log, stderr=log)
    deadline = time.monotonic() + 30
    while True:
        try:
            with urllib.request.urlopen(env["XR_GUI_URL"], timeout=1) as response:
                assert response.status == 200
            break
        except Exception:
            assert server.poll() is None, "GUI server exited"
            assert time.monotonic() < deadline, "GUI server did not start"
            time.sleep(.1)
    subprocess.run(["node", "node_modules/@playwright/test/cli.js", "test", "--config", "playwright.config.ts"],
                   cwd=gui, env=env, check=True, timeout=300)
    print("GUI evidence:", evidence)
except Exception:
    for name in cluster.profiles:
        audit = cluster.root / name / "activity.json"
        if audit.is_file():
            (evidence / (name + "-activity.json")).write_bytes(audit.read_bytes()[:512*1024])
    print("Failure evidence:", evidence)
    raise
finally:
    if server and server.poll() is None:
        server.terminate()
        server.wait(timeout=10)
    log.close()
    cluster.close()
