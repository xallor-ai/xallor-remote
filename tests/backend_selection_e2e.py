"""Real CLI/IPC/Runtime/Relay backend refusals on isolated local profiles."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import queue
import subprocess
import sys
import threading

from fixtures.runtime_cluster import Cluster

sys.stdout.reconfigure(encoding="utf-8", errors="replace")
ROOT = Path(__file__).resolve().parents[1]


class IPC:
    """One actual profile-bound Runtime IPC connection through the byte bridge."""
    def __init__(self, cluster, profile):
        self.process = subprocess.Popen(
            [str(cluster.binary), "ipc-bridge"], env=cluster.env(profile),
            stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.PIPE,
            text=True, encoding="utf-8", errors="replace", bufsize=1)
        self.frames = queue.Queue()
        self.sequence = 0
        self.reader = threading.Thread(target=self._read, daemon=True)
        self.reader.start()

    def _read(self):
        for line in self.process.stdout:
            try:
                self.frames.put(json.loads(line))
            except ValueError as error:
                self.frames.put(error)
        self.frames.put(EOFError("IPC bridge closed before terminal"))

    def call(self, method, params=None):
        self.sequence += 1
        request_id = str(self.sequence)
        self.process.stdin.write(json.dumps(
            {"id": request_id, "method": method, "params": params or {}},
            ensure_ascii=False) + "\n")
        self.process.stdin.flush()
        events = []
        while True:
            frame = self.frames.get(timeout=20)
            if isinstance(frame, Exception):
                raise frame
            assert frame.get("id") == request_id, frame
            if frame.get("event"):
                events.append(frame)
                continue
            assert isinstance(frame.get("ok"), bool), frame
            return frame, events

    def close(self):
        self.process.stdin.close()
        self.process.wait(timeout=10)
        self.reader.join(timeout=2)
        self.process.stdout.close()
        self.process.stderr.close()


def main():
    parser = argparse.ArgumentParser()
    platform = "windows-amd64/xallor-remote.exe" if os.name == "nt" else "linux-amd64/xallor-remote"
    parser.add_argument("--binary", default=str(ROOT / "dist/release" / platform))
    args = parser.parse_args()
    cluster = Cluster(args.binary)
    clients = []
    results = []
    try:
        cluster.start()
        target_id = cluster.add("target")
        cluster.add("controller")
        cluster.cli("target", "inbound", "on")
        cluster.cli("controller", "peer", "add", "--device", target_id, "--grant", cluster.grant("target"))
        target, controller = IPC(cluster, "target"), IPC(cluster, "controller")
        clients.extend([target, controller])
        frame, _ = controller.call("info", {"device_id": target_id})
        assert frame["ok"], frame
        info = json.loads(frame["result"]["content"])
        assert info["execution_backend"]["name"] == "native_user", info
        assert info["execution_backend"]["sandbox"] == "none", info
        assert info["execution_backend"]["process_tree"] == ("job_object" if os.name == "nt" else "process_group"), info
        assert [row["name"] for row in info["execution_backends"]] == ["native_user", "low_privilege", "cgroup", "container"]
        assert all(not row["available"] and row["code"] == "backend_unavailable" for row in info["execution_backends"][1:])
        results.append({"name": "target_backend_capabilities_match_os", "status": "pass"})
        config, _ = target.call("config.get")
        assert config["ok"] and config["result"]["execution_backend"] == "native_user", config
        config_path = cluster.root / "target/config.json"
        original = config_path.read_bytes()
        marker = cluster.root / "workspaces/target/backend-refused.txt"
        for backend in ("low_privilege", "cgroup", "container", "unknown"):
            frame, events = controller.call("exec", {
                "device_id": target_id, "backend": backend,
                "script": {"text": "echo forbidden > backend-refused.txt"}})
            assert not frame["ok"] and frame["code"] == "backend_unavailable", frame
            # The controller emits an empty exec-id observation before the
            # target replies; it is not proof of a started process.
            assert all(not row.get("data") for row in events) and not marker.exists(), (events, str(marker))
            frame, _ = target.call("config.set", {
                "execution_backend": backend, "relay_url": "ws://changed.invalid",
                "workspace": str(cluster.root / "must-not-create")})
            assert not frame["ok"] and frame["code"] == "backend_unavailable", frame
            assert target.call("config.get")[0]["result"] == config["result"]
            assert config_path.read_bytes() == original
            assert not (cluster.root / "must-not-create").exists()
            assert not (cluster.root / "target/jobs.json").exists()
        results.append({"name": "ipc_backend_refusal_has_no_start_or_configuration_effect", "status": "pass"})
        refused = cluster.cli("controller", "exec", "--backend", "container", "--jsonl", "--",
                              "echo forbidden > backend-refused.txt", check=False)
        rows = [json.loads(line) for line in refused.stdout.splitlines() if line]
        assert refused.returncode != 0 and rows[-1]["type"] == "error" and rows[-1]["code"] == "backend_unavailable", rows
        assert sum(row.get("type") == "error" for row in rows) == 1, rows
        assert not any(row.get("data") or row.get("type") == "exit" for row in rows), rows
        assert not marker.exists()
        results.append({"name": "cli_backend_refusal_is_one_structured_error", "status": "pass"})
        assert target.call("config.set", {"execution_backend": "native_user"})[0]["ok"]
        for flags in ([], ["--backend", "native_user"]):
            completed = cluster.cli("controller", "exec", "--jsonl", *flags, "--", "echo backend-native")
            rows = [json.loads(line) for line in completed.stdout.splitlines() if line]
            assert any(row.get("type") == "stdout" and "backend-native" in row.get("data", "") for row in rows), rows
            assert rows[-1]["type"] == "exit" and rows[-1]["status"] == "completed" and rows[-1]["exit_code"] == 0, rows
        results.append({"name": "explicit_and_default_native_backend_execute", "status": "pass"})
        for client in clients:
            client.close()
        clients.clear()
        cluster.stop("target")
        good_config = config_path.read_bytes()
        invalid = json.loads(good_config)
        invalid["execution_backend"] = "container"
        try:
            config_path.write_text(json.dumps(invalid), encoding="utf-8")
            refused = cluster.cli("target", "start", check=False)
            assert refused.returncode != 0 and "execution backend unavailable" in refused.stderr, refused.stderr
        finally:
            config_path.write_bytes(good_config)
        cluster.cli("target", "ensure")
        results.append({"name": "unsupported_persisted_backend_blocks_runtime_start", "status": "pass"})
    finally:
        for client in clients:
            client.close()
        cluster.close()
    evidence = {"os": sys.platform, "binary_sha256": hashlib.sha256(cluster.binary.read_bytes()).hexdigest(), "results": results}
    output = ROOT / "dist/verification/backend-selection.json"
    output.parent.mkdir(parents=True, exist_ok=True)
    output.write_text(json.dumps(evidence, indent=2), encoding="utf-8")
    print(json.dumps(evidence), flush=True)


if __name__ == "__main__":
    main()
