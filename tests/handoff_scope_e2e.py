"""Real Relay/Runtime/CLI principal isolation with an owned Cursor contract fixture."""
import argparse
import json
import os
from pathlib import Path
import subprocess
import sys
import time
import uuid
from fixtures.runtime_cluster import Cluster

sys.stdout.reconfigure(encoding="utf-8", errors="replace")
ROOT = Path(__file__).resolve().parents[1]


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--binary", required=True)
    parser.add_argument("--go", default="go")
    args = parser.parse_args()
    cluster = Cluster(args.binary)
    results = []

    def passed(name):
        results.append(dict(name=name, status="pass"))
        print(name, flush=True)

    try:
        cluster.start()
        target = cluster.add("target")
        cluster.add("controller")
        cluster.cli("target", "inbound", "on")
        grant = (cluster.root / "target/grant").read_text(encoding="utf-8")
        cluster.cli("controller", "peer", "add", "--id", target, "--grant", grant)
        workspace = cluster.root / "workspaces/target"

        def git(*values):
            return subprocess.run(["git", "-C", str(workspace), *values], check=True,
                                  capture_output=True, encoding="utf-8", errors="replace", timeout=30).stdout.strip()

        git("init", "--quiet")
        git("-c", "user.name=Test", "-c", "user.email=test@example.invalid", "commit", "--allow-empty", "--quiet", "-m", "fixture")
        fake = cluster.root / ("cursor-fixture.exe" if os.name == "nt" else "cursor-fixture")
        subprocess.run([args.go, "build", "-o", str(fake), "./tests/fixtures/cursor-cli"], cwd=ROOT, check=True, timeout=180)
        cluster.cli("target", "handoff", "configure", "--executable", str(fake))
        base = dict(workspace=str(workspace), revision=git("rev-parse", "HEAD"), agent="cursor",
                    mode="ask", prompt="Isolated scope fixture", trust_workspace=True, timeout_seconds=30)

        def request(request_id="same-id"):
            return dict(base, request_id=request_id, session_id=str(uuid.uuid4()))

        def submit(value, allowed=True):
            path = cluster.root / "request.json"
            path.write_text(json.dumps(value), encoding="utf-8")
            result = cluster.cli("controller", "handoff", "submit", "--device", target, "--request", str(path), check=allowed)
            if not allowed:
                assert result.returncode != 0 and "该会话不能用于当前身份" in result.stderr, result.stderr
                return
            return json.loads(result.stdout)

        def get(request_id, allowed=True):
            result = cluster.cli("controller", "handoff", "get", "--device", target, "--request-id", request_id, check=allowed)
            if not allowed:
                assert result.returncode != 0 and "没有此请求的交接回执" in result.stderr, result.stderr
                return
            return json.loads(result.stdout)

        def complete(value):
            submit(value)
            deadline = time.monotonic() + 40
            while True:
                receipt = get(value["request_id"])
                if receipt["state"] in ("completed", "failed", "unknown"):
                    assert receipt["state"] == "completed", receipt
                    assert receipt["session_id"] == value["session_id"]
                    return receipt
                assert time.monotonic() < deadline, receipt
                time.sleep(.1)

        def count():
            return len((cluster.root / "target/fixture-agent-count").read_text(encoding="utf-8").splitlines())

        admin = ["relay", "--data", str(cluster.root / "relay"), "principal"]
        alice = json.loads(cluster.cli("target", *admin, "create", "--name", "alice").stdout)
        bob = json.loads(cluster.cli("target", *admin, "create", "--name", "bob").stdout)

        def use(profile, identity):
            path = cluster.root / "credential.json"
            path.write_text(json.dumps(identity), encoding="utf-8")
            cluster.cli(profile, "account", "use", "--credential-file", str(path))

        anon = request()
        complete(anon)
        use("target", alice)
        cluster.cli("target", "device", "claim")
        use("controller", alice)
        get("same-id", allowed=False)
        submit(dict(anon, request_id="resume-anonymous"), allowed=False)
        own = request()
        own_receipt = complete(own)
        assert count() == 2
        passed("claim_separates_bearer_receipts_and_sessions_from_owner")

        cluster.cli("target", "device", "share", "--member", bob["membership_id"], "--allow", "info,handoff.submit,handoff.get")
        use("controller", bob)
        get("same-id", allowed=False)
        submit(dict(own, request_id="steal-session"), allowed=False)
        other = request()
        other_receipt = complete(other)
        assert other_receipt["session_id"] != own_receipt["session_id"] and count() == 3
        passed("shared_subject_cannot_query_or_resume_owner_but_can_reuse_request_id")

        renewed = json.loads(cluster.cli("target", *admin, "issue", "--membership", alice["membership_id"]).stdout)
        use("controller", renewed)
        assert submit(own) == own_receipt and count() == 3
        passed("credential_renewal_preserves_idempotency_without_reexecution")

        cluster.cli("target", "device", "share", "--member", bob["membership_id"], "--allow", "info,handoff.submit,handoff.get")
        use("controller", bob)
        assert submit(other) == other_receipt and count() == 3
        passed("fresh_share_keeps_same_subject_receipt_without_reexecution")

        cluster.stop("target")
        cluster.cli("target", "ensure")
        deadline = time.monotonic() + 45
        while cluster.cli("controller", "doctor", "--device", target, "--json", check=False).returncode:
            assert time.monotonic() < deadline, "target did not reconnect"
            time.sleep(.1)
        assert get("same-id") == other_receipt
        submit(dict(own, request_id="steal-after-restart"), allowed=False)
        passed("restart_preserves_scoped_receipts_and_completed_session_binding")

        cluster.cli("target", "device", "transfer", "--member", bob["membership_id"])
        use("controller", bob)
        get("same-id", allowed=False)
        submit(dict(other, request_id="old-session-new-owner"), allowed=False)
        new_owner = request()
        complete(new_owner)
        assert count() == 4
        use("target", bob)
        cluster.cli("target", "device", "transfer", "--member", alice["membership_id"])
        use("controller", alice)
        get("same-id", allowed=False)
        passed("transfer_and_transfer_back_do_not_restore_previous_namespace")

        records = [json.loads(p.read_text(encoding="utf-8")) for p in (cluster.root / "target/handoff/requests").glob("*.json")]
        assert len(records) == 4
        assert len({r["scope"]["authority"] for r in records}) == 1
        for r in records:
            assert "prompt" not in r["receipt"] and "token" not in r["scope"]
            assert r["scope"]["device_uid"]
        assert git("status", "--porcelain") == ""
        passed("persistent_records_bind_server_identity_without_credentials_or_prompt")
    finally:
        cluster.close()
        output = ROOT / "dist/verification/handoff-scope.json"
        output.parent.mkdir(parents=True, exist_ok=True)
        output.write_text(json.dumps(results, indent=2), encoding="utf-8")


if __name__ == "__main__":
    main()
