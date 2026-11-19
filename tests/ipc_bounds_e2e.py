"""An oversized unfinished IPC request must close only its own connection."""
import argparse
import json
import os
from pathlib import Path
import subprocess
import sys
import threading

from fixtures.mechanics_cli import add_mechanics_options, cluster_from_options, completed_report

sys.stdout.reconfigure(encoding="utf-8", errors="replace")
ROOT = Path(__file__).resolve().parents[1]


def main():
    parser = argparse.ArgumentParser()
    platform = "windows-amd64/xallor-remote.exe" if os.name == "nt" else "linux-amd64/xallor-remote"
    parser.add_argument("--binary", default=str(ROOT / "dist/release" / platform))
    add_mechanics_options(parser)
    args = parser.parse_args()
    cluster = cluster_from_options(args, ROOT / "dist/verification/ipc-bounds.json", __file__)
    bridge = None
    writer = None
    try:
        cluster.start()
        cluster.add("target")
        before = json.loads(cluster.cli("target", "runtime", "status").stdout)["running"]["pid"]
        bridge = subprocess.Popen([str(cluster.binary), "ipc-bridge"], env=cluster.env("target"),
                                  stdin=subprocess.PIPE, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL,
                                  **({"creationflags":subprocess.CREATE_NO_WINDOW} if os.name=="nt" else {}))

        def send_unfinished_record():
            try:
                # Keep stdin open: EOF from the test must not cause the closure.
                bridge.stdin.write(b"x" * (6 * (1024 * 1024 + 4096) + 8192 + 1))
                bridge.stdin.flush()
            except (BrokenPipeError, OSError):
                pass

        writer = threading.Thread(target=send_unfinished_record, daemon=True)
        writer.start()
        bridge.wait(timeout=10)
        writer.join(timeout=5)
        assert not writer.is_alive(), "oversized write did not unblock"
        after = json.loads(cluster.cli("target", "runtime", "status").stdout)["running"]["pid"]
        assert before == after, "IPC violation restarted or killed Runtime"
        assert json.loads(cluster.cli("target", "status", "--json").stdout)["online"]
        result = dict(passed=True, checks=["oversize_without_newline_closes_connection",
                                          "other_connections_and_runtime_survive"])
    finally:
        if bridge:
            if bridge.poll() is None:
                bridge.terminate()
            bridge.wait(timeout=10)
            if writer:
                writer.join(timeout=5)
            bridge.stdin.close()
        cluster.close()
    evidence = completed_report(cluster, result["checks"])
    out = ROOT / "dist/verification/ipc-bounds.json"
    out.parent.mkdir(parents=True, exist_ok=True)
    out.write_text(json.dumps(evidence, indent=2), encoding="utf-8")
    print(json.dumps(evidence), flush=True)


if __name__ == "__main__":
    main()
