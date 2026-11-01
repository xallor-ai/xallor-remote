"""Owned local Relay and isolated Runtime profiles shared by cross-layer tests."""
import json
import os
from pathlib import Path
import shutil
import socket
import stat
import subprocess
import tempfile
import time
import urllib.request


class Cluster:
    def __init__(self, binary):
        self.binary = Path(binary).resolve()
        self.root = Path(tempfile.mkdtemp(prefix="xr-cluster-")).resolve()
        (self.root / ".owner").write_text("runtime-cluster", encoding="utf-8")
        self.profiles = []
        self.relay = None
        self.log = None

    def env(self, name):
        env = dict(os.environ, XALLOR_REMOTE_DATA_DIR=str(self.root / name),
                   XALLOR_REMOTE_RELAY_URL=self.url)
        for key in ("XALLOR_REMOTE_BIN", "XALLOR_REMOTE_DEVICE_ID", "XALLOR_REMOTE_DEVICE_GRANT"):
            env.pop(key, None)
        return env

    def cli(self, name, *args, check=True):
        r = subprocess.run([str(self.binary), *args], env=self.env(name), capture_output=True,
                           encoding="utf-8", errors="replace", timeout=20)
        if check and r.returncode:
            raise AssertionError(f"{args[0]} failed: {r.stderr}")
        return r

    def start(self):
        with socket.socket() as s:
            s.bind(("127.0.0.1", 0))
            port = s.getsockname()[1]
        self.url = f"ws://127.0.0.1:{port}/remote"
        self.log = (self.root / "relay.log").open("wb")
        self.relay = subprocess.Popen([str(self.binary), "relay", "--listen", f"127.0.0.1:{port}",
                                       "--data", str(self.root / "relay")], stdout=self.log, stderr=self.log)
        deadline = time.monotonic() + 10
        while True:
            try:
                with urllib.request.urlopen(f"http://127.0.0.1:{port}/health", timeout=1) as response:
                    assert response.status == 200
                break
            except Exception:
                if time.monotonic() > deadline:
                    raise
                time.sleep(.05)
        return self

    def add(self, name):
        profile = self.root / name
        profile.mkdir()
        workspace = self.root / "workspaces" / name
        workspace.mkdir(parents=True)
        (profile / "config.json").write_text(json.dumps({"workspace":str(workspace), "relay_url":self.url}), encoding="utf-8")
        self.profiles.append(name)
        self.cli(name, "ensure")
        deadline = time.monotonic() + 10
        while True:
            status = json.loads(self.cli(name, "status", "--json").stdout)
            if status["online"]:
                return status["device_id"]
            assert time.monotonic() < deadline, "Runtime did not connect to fixture Relay"
            time.sleep(.05)

    def restart_relay(self):
        assert self.relay is not None and self.relay.poll() is not None
        from urllib.parse import urlparse
        address = urlparse(self.url).netloc
        self.relay = subprocess.Popen([str(self.binary), "relay", "--listen", address,
                                       "--data", str(self.root / "relay")], stdout=self.log, stderr=self.log)

    def stop(self, name):
        self.cli(name, "stop", check=False)
        deadline = time.monotonic() + 10
        while json.loads(self.cli(name, "runtime", "status").stdout)["running"] is not None:
            assert time.monotonic() < deadline, "test Runtime did not stop"
            time.sleep(.05)

    def close(self):
        errors = []
        for name in self.profiles:
            try:
                self.stop(name)
            except Exception as e:
                errors.append(str(e))
        if self.relay and self.relay.poll() is None:
            self.relay.terminate()
            self.relay.wait(timeout=10)
        if self.log:
            self.log.close()
        if errors:
            raise RuntimeError(f"Owned test profiles retained at {self.root}: {errors}")
        assert self.root.parent == Path(tempfile.gettempdir()).resolve()
        assert self.root.name.startswith("xr-cluster-") and (self.root / ".owner").read_text() == "runtime-cluster"
        assert not any(p.is_symlink() for p in self.root.rglob("*"))
        def remove_readonly(func, path, error):
            candidate = Path(path).resolve()
            if not candidate.is_relative_to(self.root) or not candidate.is_file() or not isinstance(error[1], PermissionError):
                raise error[1]
            candidate.chmod(stat.S_IREAD | stat.S_IWRITE)
            func(path)
        shutil.rmtree(self.root, onerror=remove_readonly)
