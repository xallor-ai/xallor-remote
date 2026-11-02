"""Real Runtime/Relay confirmation with an owned SQLite write barrier."""
import argparse
import json
import os
from pathlib import Path
import sqlite3
import subprocess
import sys
import time
from fixtures.runtime_cluster import Cluster

sys.stdout.reconfigure(encoding="utf-8", errors="replace")
ROOT = Path(__file__).resolve().parents[1]
parser = argparse.ArgumentParser()
platform = "windows-amd64/xallor-remote.exe" if os.name == "nt" else "linux-amd64/xallor-remote"
parser.add_argument("--binary", default=str(ROOT / "dist/release" / platform))
args = parser.parse_args()
cluster = Cluster(args.binary)
results = []
db = None
pending = None


def wait_for(predicate, description, seconds=10):
    deadline = time.monotonic() + seconds
    while not predicate():
        assert time.monotonic() < deadline, description
        time.sleep(.03)


def online():
    return json.loads(cluster.cli("target", "status", "--json").stdout)["online"]


def passed(name):
    results.append(name)
    print(name, flush=True)


try:
    cluster.start()
    target = cluster.add("target")
    cluster.add("controller")
    cluster.cli("target", "inbound", "on")
    grant_path = cluster.root / "target/grant"
    grant = grant_path.read_text(encoding="utf-8")
    cluster.cli("controller", "peer", "add", "--id", target, "--grant", grant)
    db = sqlite3.connect(cluster.root / "relay/relay.db", timeout=3)
    db.execute("BEGIN IMMEDIATE")
    pending = subprocess.Popen([str(cluster.binary), "inbound", "off"], env=cluster.env("target"),
                               stdout=subprocess.PIPE, stderr=subprocess.PIPE)
    identity_path = cluster.root / "target/identity.json"

    def locally_disabled():
        # The current local store rewrites this file; a concurrent read may see
        # an incomplete write. Retry observation only, never the mutation.
        try:
            return not json.loads(identity_path.read_text(encoding="utf-8"))["inbound"]
        except (OSError, ValueError):
            return False

    wait_for(locally_disabled, "local desired state was not saved")
    assert db.execute("SELECT inbound FROM devices WHERE id=?", (target,)).fetchone()[0] == 1
    try:
        pending.wait(timeout=.25)
    except subprocess.TimeoutExpired:
        pass
    else:
        raise AssertionError("CLI reported completion before the Relay could commit settings")
    db.commit()
    _, stderr = pending.communicate(timeout=10)
    assert pending.returncode == 0, stderr.decode("utf-8", errors="replace")
    pending = None
    assert db.execute("SELECT inbound FROM devices WHERE id=?", (target,)).fetchone()[0] == 0
    denied = cluster.cli("controller", "exec", "--device", target, "--", "echo settings-e2e", check=False)
    assert denied.returncode != 0 and "开放入站" in denied.stderr, denied.stderr
    passed("inbound_off_waits_for_commit_and_immediately_denies_execution")

    cluster.cli("target", "inbound", "on")
    executed = cluster.cli("controller", "exec", "--device", target, "--", "echo settings-e2e")
    assert "settings-e2e" in executed.stdout
    passed("inbound_on_immediately_allows_one_execution_without_retry")

    cluster.cli("target", "grant", "rotate")
    replacement = grant_path.read_text(encoding="utf-8")
    assert replacement != grant
    denied = cluster.cli("controller", "exec", "--device", target, "--", "echo old-grant", check=False)
    assert denied.returncode != 0
    cluster.cli("controller", "peer", "add", "--id", target, "--grant", replacement)
    assert "new-grant" in cluster.cli("controller", "exec", "--device", target, "--", "echo new-grant").stdout
    passed("confirmed_rotation_rejects_old_grant_and_accepts_new_grant")

    cluster.relay.terminate()
    cluster.relay.wait(timeout=10)
    wait_for(lambda: not online(), "Runtime did not observe Relay disconnect")
    result = cluster.cli("target", "inbound", "off", check=False)
    assert result.returncode != 0 and "尚未确认" in result.stderr, result.stderr
    assert locally_disabled() and grant_path.read_text(encoding="utf-8") == replacement
    cluster.restart_relay()
    wait_for(online, "saved settings were not confirmed after reconnect", seconds=45)
    assert grant_path.read_text(encoding="utf-8") == replacement
    assert db.execute("SELECT inbound FROM devices WHERE id=?", (target,)).fetchone()[0] == 0
    denied = cluster.cli("controller", "exec", "--device", target, "--", "echo still-disabled", check=False)
    assert denied.returncode != 0 and "开放入站" in denied.stderr, denied.stderr
    passed("offline_change_reports_unconfirmed_and_reconnect_preserves_exact_state")
finally:
    if db is not None:
        db.rollback()
        db.close()
    if pending is not None:
        pending.communicate(timeout=15)
    cluster.close()

output = ROOT / "dist/verification/device-settings.json"
output.parent.mkdir(parents=True, exist_ok=True)
output.write_text(json.dumps(results, indent=2), encoding="utf-8")
