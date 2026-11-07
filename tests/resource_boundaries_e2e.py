"""Full target job capacity must not consume all transport request slots."""
import argparse
import json
import os
from pathlib import Path
import subprocess
import sys
from fixtures.runtime_cluster import Cluster

sys.stdout.reconfigure(encoding="utf-8", errors="replace")
root = Path(__file__).resolve().parents[1]
parser = argparse.ArgumentParser()
platform = "windows-amd64/xallor-remote.exe" if os.name == "nt" else "linux-amd64/xallor-remote"
parser.add_argument("--binary", default=str(root/"dist/release"/platform))
parser.add_argument("--go", default="go")
args = parser.parse_args()
cluster = Cluster(args.binary)
try:
    fixture = cluster.root/("capacity.exe" if os.name == "nt" else "capacity")
    subprocess.run([args.go,"build","-o",str(fixture),"./tests/fixtures/execution-result"],cwd=root,check=True,timeout=180)
    cluster.start()
    subprocess.run(["node",str(root/"tests/helpers/transport-admission.mjs")],
                   env=dict(os.environ,XALLOR_TEST_RELAY=cluster.url),check=True,timeout=90)
    target = cluster.add("target")
    cluster.add("controller")
    cluster.cli("target","inbound","on")
    grant = cluster.grant("target")
    cluster.cli("controller","peer","add","--id",target,"--grant",grant)
    env = dict(cluster.env("controller"),XALLOR_TEST_CLUSTER=str(cluster.root),XALLOR_TEST_TARGET=target,XALLOR_TEST_PROGRAM=str(fixture))
    subprocess.run(["node",str(root/"tests/helpers/resource-boundaries.mjs")],env=env,check=True,timeout=90)
finally:
    cluster.close()
results = ["transport_saturation_and_exact_release_across_terminal_drop_reconnect", "full_foreground_capacity_keeps_info_and_files_available","additional_exec_rejected_by_target_capacity","cancel_reclaims_jobs_and_new_execution_succeeds"]
output = root/"dist/verification/resource-boundaries.json"
output.parent.mkdir(parents=True,exist_ok=True)
output.write_text(json.dumps(results,indent=2),encoding="utf-8")
print(json.dumps(results),flush=True)
