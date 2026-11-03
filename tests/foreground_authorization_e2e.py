"""Continuous foreground authorization with real Relay/Runtime/CLI processes."""
import argparse
import json
import os
from pathlib import Path
import subprocess
import sys
import time

from fixtures.file_stamp import stamp
from fixtures.mcp_foreground import verify_revocation
from fixtures.process_ref import ProcessRef
from fixtures.runtime_cluster import Cluster

sys.stdout.reconfigure(encoding="utf-8", errors="replace")
ROOT = Path(__file__).resolve().parents[1]


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--binary", required=True)
    parser.add_argument("--go", default="go")
    args = parser.parse_args()
    cluster, references, controllers, results = Cluster(args.binary), [], [], []

    def wait(check, description, seconds=35):
        deadline = time.monotonic() + seconds
        while not check():
            assert time.monotonic() < deadline, description
            time.sleep(.05)

    def passed(name):
        results.append(dict(name=name, status="pass"))
        print(name, flush=True)

    try:
        tree = cluster.root / ("tree.exe" if os.name == "nt" else "tree")
        subprocess.run([args.go, "build", "-o", str(tree), "./tests/fixtures/process-tree"], cwd=ROOT, check=True, timeout=180)
        cluster.start()
        target = cluster.add("target")
        cluster.add("controller")
        cluster.cli("target", "inbound", "on")
        cluster.cli("controller", "peer", "add", "--id", target, "--grant", cluster.grant("target"))
        profile, workspace = cluster.root / "target", cluster.root / "workspaces/target"
        admin = ["relay", "--data", str(cluster.root / "relay"), "principal"]

        def manage(*args):
            return json.loads(cluster.cli("target", *admin, *args).stdout)

        def use(name, value):
            path = cluster.root / "credential.json"
            path.write_text(json.dumps(value), encoding="utf-8")
            cluster.cli(name, "account", "use", "--credential-file", str(path))

        def start(name):
            directory = workspace / name
            directory.mkdir()
            process = subprocess.Popen([str(cluster.binary), "exec", "--jsonl", "--device", target,
                                        "--program", str(tree), "--", "parent", str(directory)],
                                       env=cluster.env("controller"), stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                                       encoding="utf-8", errors="replace")
            controllers.append(process)
            def started():
                if process.poll() is not None:
                    raise AssertionError(f"exec exited before process startup: {process.communicate()!r}")
                return (directory / "pids.json").exists() and (directory / "heartbeat").exists()
            wait(started, "tree did not start")
            pids = json.loads((directory / "pids.json").read_text(encoding="utf-8"))
            current = [ProcessRef(pids[key]) for key in ("parent", "child")]
            references.extend(current)
            assert all(ref.alive() for ref in current)
            return process, current, stamp(profile / "jobs.json")

        def stopped(worker, status="authorization_lost", seconds=35):
            process, current, before = worker
            output, error = process.communicate(timeout=seconds)
            assert process.returncode != 0, (output, error)
            terminals = [json.loads(line) for line in output.splitlines() if json.loads(line)["type"] in ("exit", "error")]
            assert len(terminals) == 1, output
            if status is not None:
                assert terminals[0]["type"] == "exit" and terminals[0]["status"] == status, terminals
                assert isinstance(terminals[0]["exit_code"], int) and terminals[0]["duration_ms"] >= 0, terminals
                assert "tree-started" in output and json.loads(output.splitlines()[-1]) == terminals[0], output
            wait(lambda: all(not ref.alive() for ref in current), "authorization loss left an owned process alive")
            wait(lambda: stamp(profile / "jobs.json") != before, "job release was not persisted")
            assert not json.loads((profile / "jobs.json").read_text(encoding="utf-8"))["jobs"]

        alice = manage("create", "--name", "alice")
        use("controller", alice)
        worker = start("credential")
        manage("revoke", "--credential", alice["credential_id"])
        stopped(worker)
        passed("credential_revocation_stops_foreground_tree_before_capacity_release")

        alice = manage("issue", "--membership", alice["membership_id"])
        use("controller", alice)
        worker = start("membership")
        manage("membership", "--membership", alice["membership_id"], "--active=false")
        manage("membership", "--membership", alice["membership_id"], "--active=true")
        stopped(worker)
        passed("member_disable_enable_does_not_revive_original_route")

        alice = manage("issue", "--membership", alice["membership_id"], "--ttl", "1m")
        use("controller", alice)
        stopped(start("expiry"), seconds=90)
        passed("credential_expiry_stops_running_foreground_command")

        cluster.cli("controller", "account", "clear")
        worker = start("anonymous")
        manage("revoke", "--credential", alice["credential_id"])
        time.sleep(22)
        assert worker[0].poll() is None and all(ref.alive() for ref in worker[1])
        passed("unlimited_anonymous_command_survives_multiple_lease_renewals")
        cluster.relay.terminate()
        cluster.relay.wait(timeout=10)
        stopped(worker, status=None)
        cluster.restart_relay()
        wait(lambda: cluster.cli("controller", "doctor", "--device", target, "--json", check=False).returncode == 0, "reconnect failed")
        assert all(not ref.alive() for ref in worker[1])
        passed("relay_disconnect_stops_foreground_without_replaying_on_reconnect")

        alice = manage("issue", "--membership", alice["membership_id"])
        bob = manage("create", "--name", "bob")
        use("target", alice)
        cluster.cli("target", "device", "claim")
        shared = json.loads(cluster.cli("target", "device", "share", "--member", bob["membership_id"], "--allow", "exec").stdout)
        use("controller", bob)
        worker = start("share")
        share = next(row["id"] for row in shared["shares"] if row["membership_id"] == bob["membership_id"])
        cluster.cli("target", "device", "unshare", "--share", share)
        cluster.cli("target", "device", "share", "--member", bob["membership_id"], "--allow", "exec")
        stopped(worker)
        passed("share_replacement_cannot_renew_original_foreground_authority")
        use("controller", bob)
        worker = start("fresh-share")
        manage("revoke", "--credential", bob["credential_id"])
        stopped(worker)
        passed("fresh_share_allows_new_route_and_rechecks_original_credential")
        bob = manage("issue", "--membership", bob["membership_id"])
        use("controller", bob)
        verify_revocation(cluster, target, tree, lambda: manage("revoke", "--credential", bob["credential_id"]))
        passed("actual_mcp_preserves_partial_output_and_confirmed_authorization_stop")
    finally:
        for process in controllers:
            if process.poll() is None:
                process.terminate()
                process.communicate(timeout=10)
        for reference in references:
            reference.close()
        cluster.close()
        output = ROOT / "dist/verification/foreground-authorization.json"
        output.parent.mkdir(parents=True, exist_ok=True)
        output.write_text(json.dumps(results, indent=2), encoding="utf-8")


if __name__ == "__main__":
    main()
