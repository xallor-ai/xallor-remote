"""Device-context changes cancel real supervised background handoff workers."""
import argparse
import json
import os
from pathlib import Path
import subprocess
import sys
import time
import uuid

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

    def wait(check, description, seconds=30):
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
        profile = cluster.root / "target"
        workspace = cluster.root / "workspaces/target"
        def pair():
            cluster.cli("controller", "peer", "add", "--id", target, "--grant", cluster.grant("target"))
        pair()
        def git(*args):
            return subprocess.check_output(["git", "-C", str(workspace), *args], encoding="utf-8").strip()
        git("init", "--quiet")
        git("-c", "user.name=Test", "-c", "user.email=test@example.invalid", "commit", "--allow-empty", "--quiet", "-m", "fixture")
        fake = cluster.root / ("agent.exe" if os.name == "nt" else "agent")
        subprocess.run([args.go, "build", "-o", str(fake), "./tests/fixtures/cursor-cli"], cwd=ROOT, check=True, timeout=180)
        cluster.cli("target", "handoff", "configure", "--executable", str(fake))
        revision = git("rev-parse", "HEAD")

        def receipt(request_id):
            for path in (profile / "handoff/requests").glob("*.json"):
                value = json.loads(path.read_text(encoding="utf-8"))["receipt"]
                if value["request_id"] == request_id:
                    return value
            return None

        def jobs():
            return json.loads((profile / "jobs.json").read_text(encoding="utf-8"))["jobs"]

        def start(name):
            for file in ("fixture-agent-pid", "fixture-descendant-pid", "fixture-descendant-ready"):
                (profile / file).unlink(missing_ok=True)
            request = dict(request_id=name, workspace=str(workspace), revision=revision, agent="cursor", session_id=str(uuid.uuid4()), mode="ask", prompt="TEST_DESCENDANT TEST_WAIT", trust_workspace=True, timeout_seconds=120)
            path = cluster.root / "request.json"
            path.write_text(json.dumps(request), encoding="utf-8")
            cluster.cli("controller", "handoff", "submit", "--device", target, "--request", str(path))
            wait(lambda: receipt(name) and receipt(name)["state"] == "running" and (profile / "fixture-descendant-pid").exists(), "worker did not start")
            current = [ProcessRef(int((profile / file).read_text())) for file in ("fixture-agent-pid", "fixture-descendant-pid")]
            references.extend(current)
            assert all(ref.alive() for ref in current) and len(jobs()) == 1
            return current

        def stopped(name, current):
            wait(lambda: all(not ref.alive() for ref in current), "revoked process tree is still alive")
            wait(lambda: receipt(name)["state"] == "unknown" and not jobs(), "revoked job did not release after cleanup")
            assert receipt(name)["code"] == "device_authorization_changed", receipt(name)

        current = start("grant-change")
        cluster.cli("target", "inbound", "on")
        cluster.relay.terminate()
        cluster.relay.wait(timeout=10)
        wait(lambda: not json.loads(cluster.cli("target", "status", "--json").stdout)["online"], "target did not notice Relay loss")
        assert all(ref.alive() for ref in current)
        cluster.restart_relay()
        wait(lambda: cluster.cli("controller", "doctor", "--device", target, "--json", check=False).returncode == 0, "device did not reconnect")
        assert all(ref.alive() for ref in current) and receipt("grant-change")["state"] == "running" and len(jobs()) == 1
        passed("idempotent_settings_and_reconnect_preserve_background_worker")
        cluster.cli("target", "grant", "rotate")
        stopped("grant-change", current)
        pair()
        passed("bearer_rotation_stops_worker_and_descendants_before_capacity_release")

        current = start("disable-inbound")
        cluster.cli("target", "inbound", "off")
        stopped("disable-inbound", current)
        cluster.cli("target", "inbound", "on")
        passed("inbound_disable_cancels_background_work_without_replay_on_enable")

        admin = ["relay", "--data", str(cluster.root / "relay"), "principal"]
        alice = json.loads(cluster.cli("target", *admin, "create", "--name", "alice").stdout)
        bob = json.loads(cluster.cli("target", *admin, "create", "--name", "bob").stdout)
        def use(profile_name, account):
            path = cluster.root / "credential.json"
            path.write_text(json.dumps(account), encoding="utf-8")
            cluster.cli(profile_name, "account", "use", "--credential-file", str(path))

        current = start("claim")
        use("target", alice)
        cluster.cli("target", "device", "claim")
        stopped("claim", current)
        passed("claim_cancels_anonymous_background_worker")

        use("controller", alice)
        current = start("transfer")
        cluster.cli("target", "device", "transfer", "--member", bob["membership_id"])
        stopped("transfer", current)
        passed("transfer_cancels_previous_owner_background_worker")

        use("controller", bob)
        current = start("transfer-back")
        cluster.cli("target", "grant", "rotate")
        assert all(ref.alive() for ref in current) and receipt("transfer-back")["state"] == "running"
        passed("claimed_device_bearer_rotation_preserves_account_worker")
        use("target", bob)
        cluster.cli("target", "device", "transfer", "--member", alice["membership_id"])
        stopped("transfer-back", current)
        passed("transfer_back_cancels_worker_without_reviving_prior_ownership")
        assert len((profile / "fixture-agent-count").read_text(encoding="utf-8").splitlines()) == 5
        assert git("status", "--porcelain") == ""
    finally:
        for ref in references:
            ref.close()
        cluster.close()
        output = ROOT / "dist/verification/handoff-revocation.json"
        output.parent.mkdir(parents=True, exist_ok=True)
        output.write_text(json.dumps(results, indent=2), encoding="utf-8")


if __name__ == "__main__":
    main()
