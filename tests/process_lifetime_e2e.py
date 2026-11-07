"""Real Runtime foreground/handoff completion must reclaim owned descendants."""
import argparse
import json
import os
from pathlib import Path
import shlex
import subprocess
import sys
import time
import uuid

from fixtures.runtime_cluster import Cluster
from fixtures.process_ref import ProcessRef

sys.stdout.reconfigure(encoding="utf-8", errors="replace")
parser = argparse.ArgumentParser()
platform = "windows-amd64/xallor-remote.exe" if os.name == "nt" else "linux-amd64/xallor-remote"
parser.add_argument("--binary", default=str(Path(__file__).resolve().parents[1]/"dist/release"/platform))
parser.add_argument("--go", default="go")
args = parser.parse_args()
repo = Path(__file__).resolve().parents[1]
cluster, refs, results = Cluster(args.binary), [], []
controller = None


def wait_file(path, process=None):
    deadline = time.monotonic() + 15
    while not path.exists():
        if process is not None and process.poll() is not None:
            raise AssertionError(f"fixture command exited before startup: {process.communicate()!r}")
        assert time.monotonic() < deadline, f"fixture did not start: {path.name}"
        time.sleep(.02)
    return path.read_text(encoding="utf-8")


def owned(pid):
    ref = ProcessRef(pid)
    refs.append(ref)
    return ref


def command(executable, mode, directory):
    values = [str(executable), mode, str(directory)]
    if os.name == "nt":
        return "& " + " ".join("'" + value.replace("'", "''") + "'" for value in values)
    return shlex.join(values)


try:
    suffix = ".exe" if os.name == "nt" else ""
    tree, agent = cluster.root / ("tree"+suffix), cluster.root / ("agent"+suffix)
    for path, source in ((tree, "process-tree"), (agent, "cursor-cli")):
        subprocess.run([args.go, "build", "-o", str(path), "./tests/fixtures/"+source], cwd=repo, check=True, timeout=180)
    cluster.start()
    target = cluster.add("target")
    cluster.add("controller")
    cluster.cli("target", "inbound", "on")
    grant = cluster.grant("target")
    cluster.cli("controller", "peer", "add", "--id", target, "--grant", grant)
    workspace = cluster.root / "workspaces/target"
    normal = workspace / "normal"
    normal.mkdir()
    result = cluster.cli("controller", "exec", "--device", target, "--", command(tree,"orphan",normal))
    assert "tree-started" in result.stdout
    pids = json.loads(wait_file(normal / "pids.json"))
    assert not owned(pids["child"]).alive(), "successful exec returned while its descendant still ran"
    results.append("foreground_success_reclaims_descendants")

    def git(*values):
        return subprocess.run(["git", "-C", str(workspace), *values], check=True, capture_output=True,
                              encoding="utf-8", errors="replace").stdout.strip()
    git("init", "--quiet")
    (workspace / ".gitignore").write_text("normal/\nlifecycle/\n", encoding="utf-8")
    git("add", ".gitignore")
    git("-c", "user.name=Test", "-c", "user.email=test@example.invalid", "commit", "-m", "fixture", "--quiet")
    cluster.cli("target", "handoff", "configure", "--executable", str(agent))
    request = dict(request_id="descendant", workspace=str(workspace), revision=git("rev-parse","HEAD"),
                   agent="cursor", session_id=str(uuid.uuid4()), mode="ask", prompt="TEST_DESCENDANT",
                   trust_workspace=True, timeout_seconds=30)
    path = cluster.root / "request.json"
    path.write_text(json.dumps(request), encoding="utf-8")
    cluster.cli("controller", "handoff", "submit", "--device", target, "--request", str(path))
    pid = int(wait_file(cluster.root / "target/fixture-descendant-pid"))
    ref = owned(pid)
    deadline = time.monotonic()+40
    while True:
        receipt=json.loads(cluster.cli("controller","handoff","get","--device",target,"--request-id","descendant").stdout)
        if receipt["state"] in ("completed","failed","unknown"):
            break
        assert time.monotonic()<deadline, receipt
        time.sleep(.05)
    assert receipt["state"]=="completed" and receipt["result"]=="handoff-fixture-ok", receipt
    assert not ref.alive(), "handoff completed before descendant reclamation"
    results.append("handoff_success_reclaims_inherited_pipe_descendant")

    shutdown=workspace/"lifecycle"
    shutdown.mkdir()
    controller=subprocess.Popen([str(cluster.binary),"exec","--device",target,"--",command(tree,"parent",shutdown)],
                                 env=cluster.env("controller"),stdout=subprocess.PIPE,stderr=subprocess.PIPE)
    pids=json.loads(wait_file(shutdown/"pids.json",controller))
    parent,child=owned(pids["parent"]),owned(pids["child"])
    assert parent.alive() and child.alive()
    cluster.stop("target")
    controller.communicate(timeout=20)
    assert not parent.alive() and not child.alive(), "Runtime exited before foreground cleanup"
    assert controller.returncode!=0, "interrupted foreground reported success"
    results.append("runtime_shutdown_waits_for_foreground_reclamation")
finally:
    if controller and controller.poll() is None:
        controller.terminate()
        controller.communicate(timeout=10)
    for ref in refs:
        ref.close()
    cluster.close()
    output=repo/"dist/verification/process-lifetime.json"
    output.parent.mkdir(parents=True,exist_ok=True)
    output.write_text(json.dumps(results,indent=2),encoding="utf-8")
print(json.dumps(results),flush=True)
