"""Shared local occupancy follows real jobs and survives a killed Runtime."""
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
root=Path(__file__).resolve().parents[1]
parser=argparse.ArgumentParser()
platform="windows-amd64/xallor-remote.exe" if os.name=="nt" else "linux-amd64/xallor-remote"
parser.add_argument("--binary",default=str(root/"dist/release"/platform))
parser.add_argument("--go",default="go")
args=parser.parse_args()
cluster=Cluster(args.binary)
controllers=[]
refs=[]
results=[]


def wait(predicate, description, seconds=30):
    deadline=time.monotonic()+seconds
    while True:
        value=predicate()
        if value:
            return value
        assert time.monotonic()<deadline, description
        time.sleep(.05)


def owned(pid):
    ref=ProcessRef(pid)
    refs.append(ref)
    return ref


def jobs():
    return json.loads((cluster.root/"target/jobs.json").read_text(encoding="utf-8"))["jobs"]


def launch_tree(index):
    folder=workspace/("tree"+str(index))
    folder.mkdir()
    values=[str(tree),"parent",str(folder)]
    command=("& "+" ".join("'"+v.replace("'","''")+"'" for v in values)) if os.name=="nt" else shlex.join(values)
    child=subprocess.Popen([str(cluster.binary),"exec","--device",target,"--",command],env=cluster.env("controller"),stdout=subprocess.PIPE,stderr=subprocess.PIPE)
    controllers.append(child)
    def ready():
        if child.poll() is not None:
            raise AssertionError(f"tree failed before start: {child.communicate()!r}")
        try:
            return json.loads((folder/"pids.json").read_text(encoding="utf-8"))
        except (FileNotFoundError, PermissionError):
            # The fixture publishes by rename; Windows can briefly deny a
            # reader while that replacement handle is still closing.
            return None
    pids=wait(ready,"tree PID publication did not become readable")
    parent,descendant=owned(pids["parent"]),owned(pids["child"])
    return child,parent,descendant


try:
    suffix=".exe" if os.name=="nt" else ""
    tree,agent=cluster.root/("tree"+suffix),cluster.root/("agent"+suffix)
    reservations=cluster.root/("reservations"+suffix)
    for path,source in ((tree,"process-tree"),(agent,"cursor-cli"),(reservations,"job-reservations")):
        subprocess.run([args.go,"build","-o",str(path),"./tests/fixtures/"+source],cwd=root,check=True,timeout=180)
    subprocess.run([str(reservations),"sequence",str(cluster.root/"sequence")],check=True,timeout=30)
    results.append("one_owner_rejects_parallel_launch_and_reuses_slot_after_cleanup")
    cluster.start()
    target=cluster.add("target")
    cluster.add("controller")
    cluster.cli("target","inbound","on")
    grant=cluster.grant("target")
    cluster.cli("controller","peer","add","--id",target,"--grant",grant)
    cluster.stop("target")
    ready=cluster.root/"reservations-ready.json"
    reserver=subprocess.Popen([str(reservations),str(cluster.root/"target"),str(ready)],stdout=subprocess.PIPE,stderr=subprocess.PIPE)
    controllers.append(reserver)
    def reserved():
        if reserver.poll() is not None:
            raise AssertionError(f"reservation fixture failed: {reserver.communicate()!r}")
        try:
            return json.loads(ready.read_text(encoding="utf-8"))
        except (FileNotFoundError,PermissionError):
            return None
    records=wait(reserved,"reservation fixture never published readiness")
    assert len(records)==4 and all(r["state"]=="reserved" for r in records),records
    owned(reserver.pid).close()
    reserver.communicate(timeout=10)
    cluster.cli("target","ensure")
    wait(lambda:cluster.cli("controller","doctor","--device",target,"--json",check=False).returncode==0,"Runtime did not reconnect after lost reservations",45)
    recovered=cluster.cli("controller","exec","--device",target,"--","echo reservation-recovered")
    assert "reservation-recovered" in recovered.stdout,recovered
    assert jobs()=={},jobs()
    results.append("killed_reservation_owner_recovers_capacity_without_replaying_work")
    workspace=cluster.root/"workspaces/target"
    def git(*values):
        return subprocess.run(["git","-C",str(workspace),*values],check=True,capture_output=True,encoding="utf-8",errors="replace").stdout.strip()
    git("init","--quiet")
    (workspace/".gitignore").write_text("tree*/\n",encoding="utf-8")
    git("add",".gitignore")
    git("-c","user.name=Test","-c","user.email=test@example.invalid","commit","-m","fixture","--quiet")
    revision=git("rev-parse","HEAD")
    cluster.cli("target","handoff","configure","--executable",str(agent))
    for i in range(2):
        request=dict(request_id="job"+str(i),workspace=str(workspace),revision=revision,agent="cursor",session_id=str(uuid.uuid4()),mode="ask",prompt="TEST_JOB_WAIT",trust_workspace=True,timeout_seconds=300)
        path=cluster.root/("request"+str(i)+".json")
        path.write_text(json.dumps(request),encoding="utf-8")
        cluster.cli("controller","handoff","submit","--device",target,"--request",str(path))
        def running():
            receipt=json.loads(cluster.cli("controller","handoff","get","--device",target,"--request-id",request["request_id"]).stdout)
            assert receipt["state"] not in ("failed","unknown","completed"),receipt
            return receipt["state"]=="running"
        wait(running,"handoff did not enter running",60)
        owned(int((cluster.root/"target/fixture-agent-pid").read_text()))
    first,parent,descendant=launch_tree(1)
    launch_tree(2)
    assert len(jobs())==4
    denied=cluster.cli("controller","exec","--device",target,"--","echo should-not-start",check=False)
    assert denied.returncode!=0 and "已达到资源上限" in denied.stderr and "should-not-start" not in denied.stdout,denied
    results.append("foreground_and_handoff_share_local_capacity")
    first.terminate()
    first.communicate(timeout=20)
    wait(lambda:len(jobs())==3,"cancelled job did not release")
    assert not parent.alive() and not descendant.alive()
    assert "released-slot" in cluster.cli("controller","exec","--device",target,"--","echo released-slot").stdout
    wait(lambda:len(jobs())==3,"short execution left occupancy")
    results.append("confirmed_cleanup_releases_once_for_next_job")
    launch_tree(3)
    assert len(jobs())==4
    old_ids=set(jobs())
    runtime=json.loads(cluster.cli("target","runtime","status").stdout)["running"]
    owned(runtime["pid"]).close()
    for control in controllers:
        if control.poll() is None:
            control.communicate(timeout=20)
    cluster.cli("target","ensure")
    wait(lambda:cluster.cli("controller","doctor","--device",target,"--json",check=False).returncode==0,"Runtime did not reconnect",45)
    denied=cluster.cli("controller","exec","--device",target,"--","echo should-not-replay",check=False)
    assert denied.returncode!=0 and "已达到资源上限" in denied.stderr and "should-not-replay" not in denied.stdout,denied
    retained=jobs()
    assert set(retained)==old_ids and all(r["state"]=="uncertain" for r in retained.values()),retained
    for i in range(2):
        receipt=json.loads(cluster.cli("controller","handoff","get","--device",target,"--request-id","job"+str(i)).stdout)
        assert receipt["state"]=="unknown" and receipt["code"]=="runtime_restarted",receipt
    assert len((cluster.root/"target/fixture-agent-count").read_text().splitlines())==2
    results.append("runtime_crash_keeps_uncertain_occupancy_without_replay")
finally:
    for control in controllers:
        if control.poll() is None:
            control.terminate()
            control.communicate(timeout=10)
    for ref in refs:
        ref.close()
    cluster.close()
    output=root/"dist/verification/job-lifecycle.json"
    output.parent.mkdir(parents=True,exist_ok=True)
    output.write_text(json.dumps(results,indent=2),encoding="utf-8")
print(json.dumps(results),flush=True)
