"""Verify one intentional negative teardown; never release product job debt."""
import hashlib
import json
from pathlib import Path
import tempfile

from .runtime_cluster import ClusterCleanupError, StopUnconfirmed


def verify_expected_retention(cluster, error, owner, before, child_exits, results):
    assert isinstance(error, ClusterCleanupError)
    assert error.root == cluster.root and set(error.failures) == {"target"}
    denial = error.failures["target"]
    assert isinstance(denial, StopUnconfirmed) and denial.profile == "target"
    # The CLI has no structured stop-error envelope here. Match the specific
    # durable-job refusal, not the shared sentinel also used for missing ACKs,
    # observer errors and uncertain process exit.
    expected = "runtime cleanup is uncertain：任务资源释放尚未确认，请保留现有记录并检查任务进程。"
    assert denial.returncode != 0 and denial.stderr.strip() == expected
    assert not cluster.cleanup_complete and results
    assert not owner.alive(), "retained native owner reference has not signalled exit"
    assert child_exits and all(value is True for value in child_exits)
    root = cluster.root
    assert root.parent == Path(tempfile.gettempdir()).resolve() and root.name.startswith("xr-cluster-")
    assert not root.is_symlink() and not any(path.is_symlink() for path in root.rglob("*"))
    assert (root/".owner").read_text(encoding="utf-8") == "runtime-cluster"
    after = (root/"target/jobs.json").read_bytes()
    assert after == before, "stop changed the deliberately retained job evidence"
    records = json.loads(after)["jobs"]
    assert records and all(record["state"] == "uncertain" for record in records.values())
    return {"status":"expected_refusal_verified", "evidence":dict(cluster.evidence),
            "results":results, "cleanup_complete":False, "test_processes_exited":True,
            "observed_child_exits":len(child_exits), "retained_root":str(root),
            "retained_jobs_sha256":hashlib.sha256(after).hexdigest(),
            "retained_job_count":len(records), "expected_stop_error":denial.stderr,
            "product_job_release_confirmed":False}
