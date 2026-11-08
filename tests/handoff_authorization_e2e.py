"""Continuous authorization through real Relay/Runtime/CLI and owned workers."""
import argparse
import json
import os
from pathlib import Path
import subprocess
import sys
import time
import uuid

from fixtures.file_stamp import stamp
from fixtures.process_ref import ProcessRef
from fixtures.runtime_cluster import Cluster

sys.stdout.reconfigure(encoding="utf-8", errors="replace")
ROOT = Path(__file__).resolve().parents[1]


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--binary", required=True)
    parser.add_argument("--go", default="go")
    args = parser.parse_args()
    cluster, references, results = Cluster(args.binary), [], []

    def wait(check, description, seconds=35):
        deadline = time.monotonic() + seconds
        while True:
            value = check()
            if value:
                return value
            assert time.monotonic() < deadline, description
            time.sleep(.05)

    def passed(name):
        results.append(dict(name=name, status="pass"))
        print(name, flush=True)

    try:
        cluster.start()
        target = cluster.add("target")
        cluster.add("controller")
        cluster.cli("target", "inbound", "on")
        cluster.cli("controller", "peer", "add", "--device", target, "--grant", cluster.grant("target"))
        profile, workspace = cluster.root / "target", cluster.root / "workspaces/target"

        def git(*args):
            return subprocess.check_output(["git", "-C", str(workspace), *args], encoding="utf-8").strip()

        git("init", "--quiet")
        git("-c", "user.name=Test", "-c", "user.email=test@example.invalid", "commit", "--allow-empty", "--quiet", "-m", "fixture")
        fake = cluster.root / ("agent.exe" if os.name == "nt" else "agent")
        subprocess.run([args.go, "build", "-o", str(fake), "./tests/fixtures/cursor-cli"], cwd=ROOT, check=True, timeout=180)
        cluster.cli("target", "handoff", "configure", "--executable", str(fake))
        revision = git("rev-parse", "HEAD")
        admin = ["relay", "--data", str(cluster.root / "relay"), "principal"]

        def manage(*args):
            return json.loads(cluster.cli("target", *admin, *args).stdout)

        def use(name, value):
            path = cluster.root / "credential.json"
            path.write_text(json.dumps(value), encoding="utf-8")
            cluster.cli(name, "account", "use", "--credential-file", str(path))

        def get(name):
            return json.loads(cluster.cli("controller", "handoff", "get", "--device", target, "--request-id", name).stdout)

        def start(name):
            for file in ("fixture-agent-pid", "fixture-descendant-pid", "fixture-descendant-ready"):
                (profile / file).unlink(missing_ok=True)
            request = dict(request_id=name, workspace=str(workspace), revision=revision, agent="cursor", session_id=str(uuid.uuid4()), mode="ask", prompt="TEST_DESCENDANT TEST_WAIT", trust_workspace=True, timeout_seconds=120)
            path = cluster.root / "request.json"
            path.write_text(json.dumps(request), encoding="utf-8")
            cluster.cli("controller", "handoff", "submit", "--device", target, "--request", str(path))
            wait(lambda: get(name)["state"] == "running" and (profile / "fixture-descendant-ready").exists(), "worker did not start")
            current = [ProcessRef(int((profile / file).read_text())) for file in ("fixture-agent-pid", "fixture-descendant-pid")]
            references.extend(current)
            receipt_file = next(file for file in (profile / "handoff/requests").glob("*.json") if json.loads(file.read_text(encoding="utf-8"))["receipt"]["request_id"] == name)
            return current, receipt_file, stamp(receipt_file)

        def stopped(worker):
            current, file, before = worker
            wait(lambda: all(not ref.alive() for ref in current), "authorization loss did not stop supervised tree")
            wait(lambda: stamp(file) != before, "terminal receipt missing")
            receipt = json.loads(file.read_text(encoding="utf-8"))["receipt"]
            assert receipt["state"] == "unknown" and receipt["code"] == "execution_authorization_lost", receipt
            assert not json.loads((profile / "jobs.json").read_text(encoding="utf-8"))["jobs"]

        alice = manage("create", "--name", "alice")
        use("controller", alice)
        worker = start("credential")
        manage("revoke", "--credential", alice["credential_id"])
        stopped(worker)
        passed("credential_revocation_stops_account_worker_on_unclaimed_device")

        alice = manage("issue", "--membership", alice["membership_id"])
        use("controller", alice)
        worker = start("membership")
        manage("membership", "--membership", alice["membership_id"], "--active=false")
        manage("membership", "--membership", alice["membership_id"], "--active=true")
        stopped(worker)
        passed("member_disable_enable_does_not_revive_original_delegation")

        cluster.cli("controller", "account", "clear")
        worker = start("anonymous")
        manage("revoke", "--credential", alice["credential_id"])
        time.sleep(7)
        assert all(ref.alive() for ref in worker[0]) and get("anonymous")["state"] == "running"
        passed("unrelated_account_revocation_preserves_anonymous_grant_worker")
        cluster.relay.terminate()
        cluster.relay.wait(timeout=10)
        wait(lambda: not json.loads(cluster.cli("target", "status", "--json").stdout)["online"], "target did not notice disconnect")
        cluster.restart_relay()
        wait(lambda: cluster.cli("controller", "doctor", "--device", target, "--json", check=False).returncode == 0, "reconnect failed")
        time.sleep(7)
        assert all(ref.alive() for ref in worker[0]) and get("anonymous")["state"] == "running"
        passed("relay_restart_retains_signing_key_and_renews_existing_worker")
        cluster.relay.terminate()
        cluster.relay.wait(timeout=10)
        stopped(worker)
        passed("network_partition_expires_last_lease_without_releasing_live_processes")
        cluster.restart_relay()
        wait(lambda: cluster.cli("controller", "doctor", "--device", target, "--json", check=False).returncode == 0, "second reconnect failed", seconds=45)

        alice = manage("issue", "--membership", alice["membership_id"])
        bob = manage("create", "--name", "bob")
        use("target", alice)
        cluster.cli("target", "device", "claim")
        shared = json.loads(cluster.cli("target", "device", "share", "--member", bob["membership_id"], "--allow", "info,handoff.submit,handoff.get").stdout)
        use("controller", bob)
        worker = start("share")
        share_id = next(row["id"] for row in shared["shares"] if row["membership_id"] == bob["membership_id"])
        cluster.cli("target", "device", "unshare", "--share", share_id)
        cluster.cli("target", "device", "share", "--member", bob["membership_id"], "--allow", "info,handoff.submit,handoff.get")
        stopped(worker)
        passed("share_replacement_does_not_revive_original_background_authorization")

        use("controller", bob)
        worker = start("fresh-share")
        manage("revoke", "--credential", bob["credential_id"])
        stopped(worker)
        passed("fresh_share_allows_new_work_and_owned_worker_observes_credential_revocation")
        assert git("status", "--porcelain") == ""
        for file in (profile / "handoff/requests").glob("*.json"):
            text = file.read_text(encoding="utf-8")
            assert "delegation" not in text and "xr_account_" not in text and "TEST_DESCENDANT" not in text
        passed("persisted_receipts_exclude_proofs_credentials_and_prompts")
    finally:
        for ref in references:
            ref.close()
        cluster.close()
        output = ROOT / "dist/verification/handoff-authorization.json"
        output.parent.mkdir(parents=True, exist_ok=True)
        output.write_text(json.dumps(results, indent=2), encoding="utf-8")


if __name__ == "__main__":
    main()
