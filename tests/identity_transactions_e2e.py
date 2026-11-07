"""Real CLI/Runtime/Relay with isolated profile migration and local write faults."""
import argparse
import json
import os
from pathlib import Path
import signal
import sqlite3
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


def passed(name):
    results.append(name)
    print(name, flush=True)


def wait_for(predicate, description):
    deadline = time.monotonic() + 15
    while not predicate():
        assert time.monotonic() < deadline, description
        time.sleep(.05)


def online():
    return json.loads(cluster.cli("target", "status", "--json").stdout)["online"]


def execute(marker):
    result = cluster.cli("controller", "exec", "--device", target, "--", "echo " + marker)
    assert marker in result.stdout


try:
    cluster.start()
    profile = cluster.root / "target"
    profile.mkdir()
    workspace = cluster.root / "workspaces/target"
    workspace.mkdir(parents=True)
    target = "790442262"
    original_secret, original_grant = "a" * 64, "xr_grant_" + "b" * 32
    for name, content in {
        "identity.json": json.dumps({"device_id": target, "inbound": True}),
        "device_secret": original_secret,
        "grant": original_grant,
        "config.json": json.dumps({"workspace": str(workspace), "relay_url": cluster.url}),
    }.items():
        (profile / name).write_text(content, encoding="utf-8")
    cluster.profiles.append("target")
    cluster.cli("target", "ensure")
    wait_for(online, "migrated Runtime did not connect")
    current = cluster.device_state("target")
    assert current["device_id"] == target and current["secret"] == original_secret
    assert current["grant"] == original_grant and current["inbound"] == 1
    assert not any((profile / name).exists() for name in ("identity.json", "device_secret", "grant"))
    cluster.add("controller")
    cluster.cli("controller", "peer", "add", "--id", target, "--grant", original_grant)
    execute("migration-keeps-access")
    passed("one_time_migration_preserves_identity_and_real_remote_access")

    db = sqlite3.connect(profile / "identity.db", timeout=3)
    db.execute("CREATE TRIGGER fail_settings BEFORE UPDATE ON identity BEGIN SELECT RAISE(ABORT, 'test-owned write fault'); END")
    db.commit()
    for command in (("grant", "rotate"), ("inbound", "off"), ("revoke",)):
        failed = cluster.cli("target", *command, check=False)
        assert failed.returncode != 0, "local write failure was reported as success"
        assert cluster.device_state("target") == current, "failed write changed durable identity"
        assert original_grant in cluster.cli("target", "grant", "show").stdout, "failed write changed Runtime cache"
        execute("failed-write-keeps-access")
    db.execute("DROP TRIGGER fail_settings")
    db.commit()
    passed("failed_local_transactions_preserve_cache_disk_and_relay_access")

    db.execute("BEGIN IMMEDIATE")
    failed = cluster.cli("target", "inbound", "off", check=False)
    assert failed.returncode != 0
    assert cluster.device_state("target") == current
    db.rollback()
    execute("busy-write-keeps-access")
    passed("contended_local_database_fails_without_partial_settings")

    cluster.cli("target", "grant", "rotate")
    replacement = cluster.grant("target")
    assert replacement != original_grant
    cluster.cli("controller", "peer", "add", "--id", target, "--grant", replacement)
    execute("committed-rotation")
    cluster.cli("target", "inbound", "off")
    saved = cluster.device_state("target")
    # Only terminate the PID reported by the fixture's isolated profile. No jobs
    # are active; this checks restart after a confirmed committed local mutation.
    state = json.loads(cluster.cli("target", "runtime", "status").stdout)
    owned_pid = state["running"]["pid"]
    os.kill(owned_pid, signal.SIGTERM)
    wait_for(lambda: json.loads(cluster.cli("target", "runtime", "status").stdout)["running"] is None,
             "owned Runtime did not terminate")
    cluster.cli("target", "ensure")
    wait_for(online, "Runtime did not reconnect after abrupt stop")
    assert cluster.device_state("target") == saved
    denied = cluster.cli("controller", "exec", "--device", target, "--", "echo must-not-run", check=False)
    assert denied.returncode != 0 and "开放入站" in denied.stderr
    cluster.cli("target", "inbound", "on")
    assert cluster.grant("target") == replacement
    execute("restart-keeps-committed-state")
    passed("abrupt_restart_keeps_committed_grant_and_disabled_inbound")

    saved = cluster.device_state("target")
    db.execute("CREATE TRIGGER fail_reset BEFORE UPDATE ON identity BEGIN SELECT RAISE(ABORT, 'test-owned reset fault'); END")
    db.commit()
    failed = cluster.cli("target", "reset", "--yes", check=False)
    assert failed.returncode != 0 and "重置未完成" in failed.stderr, "failed reset was not reported"
    wait_for(lambda: json.loads(cluster.cli("target", "runtime", "status").stdout)["running"] is None,
             "Runtime remained live after failed reset")
    assert cluster.device_state("target") == saved
    db.execute("DROP TRIGGER fail_reset")
    db.commit()
    passed("failed_reset_returns_error_and_stops_without_replacing_identity")

    db.execute("UPDATE identity SET device_id='corrupt'")
    db.commit()
    before = (profile / "identity.db").read_bytes()
    failed = cluster.cli("target", "ensure", check=False)
    assert failed.returncode != 0, "corrupt identity silently regenerated"
    assert (profile / "identity.db").read_bytes() == before
    assert json.loads(cluster.cli("target", "runtime", "status").stdout)["running"] is None
    passed("corrupt_canonical_identity_blocks_startup_without_regeneration")
finally:
    if db is not None:
        db.rollback()
        db.close()
    cluster.close()

output = ROOT / "dist/verification/identity-transactions.json"
output.parent.mkdir(parents=True, exist_ok=True)
output.write_text(json.dumps(results, indent=2), encoding="utf-8")
