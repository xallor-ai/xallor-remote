"""Owned local Relay and isolated Runtime profiles shared by cross-layer tests."""
import json
import hashlib
import os
from pathlib import Path
import shutil
import socket
import sqlite3
import stat
import subprocess
import tempfile
import time
import urllib.request


class StopUnconfirmed(RuntimeError):
    def __init__(self, profile, returncode, stderr):
        self.profile, self.returncode, self.stderr = profile, returncode, stderr
        super().__init__(f"Owned Runtime stop unconfirmed; retain profile {profile}: {stderr}")


class ClusterCleanupError(RuntimeError):
    def __init__(self, root, failures):
        self.root, self.failures = Path(root), dict(failures)
        super().__init__(f"Owned test profiles retained at {root}: {[str(error) for error in failures.values()]}")


def device_state(profile):
    """Read only the canonical local state; tests never fall back to old files."""
    db = sqlite3.connect((Path(profile) / "identity.db").resolve().as_uri() + "?mode=ro", timeout=3, uri=True)
    try:
        db.row_factory = sqlite3.Row
        return dict(db.execute("SELECT device_id,secret,grant,inbound,active,revision FROM identity WHERE singleton=1").fetchone())
    finally:
        db.close()


class Cluster:
    def __init__(self, binary, *, mechanics_go=None, mechanics_binary=None):
        self.binary = Path(binary).resolve()
        self.root = Path(tempfile.mkdtemp(prefix="xr-cluster-")).resolve()
        (self.root / ".owner").write_text("runtime-cluster", encoding="utf-8")
        self.profiles = []
        self.relay = None
        self.log = None
        self.fixture_env = {}
        self.cleanup_complete = False
        self.evidence = {"mode": "supplied-runtime", "cryptographic_acceptance": "requires separate source evidence"}
        if mechanics_go is not None or mechanics_binary is not None:
            from .owner_mechanics import build_owner_mechanics
            # Build failure retains the owned root for inspection. No Runtime
            # or Relay has been launched at this point.
            self.binary, self.fixture_env, self.evidence = build_owner_mechanics(self.root, mechanics_go, mechanics_binary)

    def base_env(self):
        env = {key:value for key,value in os.environ.items()
               if not key.upper().startswith(("XALLOR_", "XR_TEST_"))}
        if self.fixture_env:
            env = {key:value for key,value in env.items() if key.lower() != "path"}
        env.update(self.fixture_env)
        return env

    def device_state(self, name):
        return device_state(self.root / name)

    def grant(self, name):
        return self.device_state(name)["grant"]

    def env(self, name):
        env = dict(self.base_env(), XALLOR_REMOTE_DATA_DIR=str(self.root / name),
                   XALLOR_REMOTE_RELAY_URL=self.url)
        for key in ("XALLOR_REMOTE_BIN", "XALLOR_REMOTE_DEVICE_ID", "XALLOR_REMOTE_DEVICE_GRANT"):
            env.pop(key, None)
        return env

    def cli(self, name, *args, check=True):
        quiet = {"creationflags":subprocess.CREATE_NO_WINDOW} if os.name=="nt" else {}
        budget = 150 if args and args[0] in ("start", "ensure", "runtime") else 75
        r = subprocess.run([str(self.binary), *args], env=self.env(name), capture_output=True,
                           encoding="utf-8", errors="replace", timeout=budget, **quiet)
        if check and r.returncode:
            raise AssertionError(f"{args[0]} failed: {r.stderr}")
        return r

    def start(self):
        self.evidence["binary_sha256"] = hashlib.sha256(self.binary.read_bytes()).hexdigest()
        with socket.socket() as s:
            s.bind(("127.0.0.1", 0))
            port = s.getsockname()[1]
        self.url = f"ws://127.0.0.1:{port}/remote"
        self.log = (self.root / "relay.log").open("wb")
        self.relay = subprocess.Popen([str(self.binary), "relay", "--listen", f"127.0.0.1:{port}",
                                       "--data", str(self.root / "relay")], stdout=self.log, stderr=self.log,
                                      env=self.base_env(), **({"creationflags":subprocess.CREATE_NO_WINDOW} if os.name=="nt" else {}))
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
                                       "--data", str(self.root / "relay")], stdout=self.log, stderr=self.log,
                                      env=self.base_env(), **({"creationflags":subprocess.CREATE_NO_WINDOW} if os.name=="nt" else {}))

    def stop(self, name):
        stopped = self.cli(name, "stop", check=False)
        if stopped.returncode != 0:
            raise StopUnconfirmed(name, stopped.returncode, stopped.stderr)
        deadline = time.monotonic() + 10
        while json.loads(self.cli(name, "runtime", "status").stdout)["running"] is not None:
            assert time.monotonic() < deadline, "test Runtime did not stop"
            time.sleep(.05)

    def close(self):
        errors = {}
        for name in self.profiles:
            try:
                self.stop(name)
            except Exception as e:
                errors[name] = e
        if self.relay and self.relay.poll() is None:
            self.relay.terminate()
            self.relay.wait(timeout=10)
        if self.log:
            self.log.close()
        if errors:
            raise ClusterCleanupError(self.root, errors)
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
        self.cleanup_complete = True
