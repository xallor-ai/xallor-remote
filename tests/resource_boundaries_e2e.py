"""Full target job capacity must not consume all transport request slots."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import subprocess
import shutil
import sys
from fixtures.mechanics_cli import add_mechanics_options, cluster_from_options, completed_report

sys.stdout.reconfigure(encoding="utf-8", errors="replace")
root = Path(__file__).resolve().parents[1]
parser = argparse.ArgumentParser()
platform = "windows-amd64/xallor-remote.exe" if os.name == "nt" else "linux-amd64/xallor-remote"
parser.add_argument("--binary", default=str(root/"dist/release"/platform))
parser.add_argument("--go", default="go")
add_mechanics_options(parser, add_go=False)
parser.add_argument("--program-fixture", help="Explicit trusted precompiled execution-result test program")
args = parser.parse_args()
output = root/"dist/verification/resource-boundaries.json"
cluster = cluster_from_options(args, output, __file__)
quiet = {"creationflags":subprocess.CREATE_NO_WINDOW} if os.name == "nt" else {}

def run_helper(name, env):
    observed = subprocess.run(["node",str(root/"tests/helpers"/name)],env=env,
                              capture_output=True,text=True,encoding="utf-8",errors="replace",timeout=90,**quiet)
    assert observed.returncode == 0, (observed.stdout,observed.stderr)
    print(observed.stdout,end="",flush=True)

try:
    fixture = cluster.root/("capacity.exe" if os.name == "nt" else "capacity")
    if args.program_fixture:
        shutil.copy2(Path(args.program_fixture).resolve(),fixture)
        fixture.chmod(0o700)
    else:
        subprocess.run([args.go,"build","-o",str(fixture),"./tests/fixtures/execution-result"],cwd=root,check=True,timeout=180,**quiet)
    cluster.evidence["program_fixture_sha256"] = hashlib.sha256(fixture.read_bytes()).hexdigest()
    cluster.start()
    run_helper("transport-admission.mjs",dict(cluster.base_env(),XALLOR_TEST_RELAY=cluster.url))
    target = cluster.add("target")
    cluster.add("controller")
    cluster.cli("target","inbound","on")
    grant = cluster.grant("target")
    cluster.cli("controller","peer","add","--device",target,"--grant",grant)
    env = dict(cluster.env("controller"),XALLOR_TEST_CLUSTER=str(cluster.root),XALLOR_TEST_TARGET=target,XALLOR_TEST_PROGRAM=str(fixture))
    run_helper("resource-boundaries.mjs",env)
finally:
    cluster.close()
results = ["transport_saturation_and_exact_release_across_terminal_drop_reconnect", "full_foreground_capacity_keeps_info_and_files_available","additional_exec_rejected_by_target_capacity","cancel_reclaims_jobs_and_new_execution_succeeds"]
output.write_text(json.dumps(completed_report(cluster,results),indent=2),encoding="utf-8")
print(json.dumps(results),flush=True)
