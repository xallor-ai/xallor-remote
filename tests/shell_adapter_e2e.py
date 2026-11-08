"""Target-selected shells over real IPC/Relay, with CLI config and restart."""
import argparse
import json
import os
import shutil
from pathlib import Path
import subprocess
import sys
import time
from fixtures.runtime_cluster import Cluster

sys.stdout.reconfigure(encoding="utf-8", errors="replace")
root = Path(__file__).resolve().parents[1]
parser = argparse.ArgumentParser()
platform = "windows-amd64/xallor-remote.exe" if os.name == "nt" else "linux-amd64/xallor-remote"
parser.add_argument("--binary", default=str(root/"dist/release"/platform))
args = parser.parse_args()
cluster = Cluster(args.binary)
try:
    cluster.start()
    target = cluster.add("target")
    cluster.add("controller")
    cluster.cli("target", "inbound", "on")
    grant = cluster.grant("target")
    cluster.cli("controller", "peer", "add", "--device", target, "--grant", grant)
    selected = "powershell" if os.name == "nt" else "sh"
    cluster.cli("target", "config", "set", "--shell", selected)
    assert json.loads(cluster.cli("target", "config", "get").stdout)["shell"] == selected
    rejected = cluster.cli("target", "config", "set", "--shell", "arbitrary --command", check=False)
    assert rejected.returncode != 0
    assert json.loads(cluster.cli("target", "config", "get").stdout)["shell"] == selected
    env = dict(cluster.env("controller"), XALLOR_TEST_CLUSTER=str(cluster.root),
               XALLOR_TEST_TARGET=target, XALLOR_TEST_SHELL=selected)
    subprocess.run(["node", str(root/"tests/helpers/shell-adapter.mjs")], env=env, check=True, timeout=180)
    script_file = cluster.root / "declarations ' 中文.ps1"
    script_file.write_text("using namespace System.Text\nparam([string]$Label = 'script-file-ready 中文🌍')\nbegin { $saved = $Label } end { [StringBuilder]::new($saved).ToString() }", encoding="utf-8")
    available = json.loads((root/"dist/verification/shell-adapter.json").read_text(encoding="utf-8"))
    for row in available:
        if row["name"] in ("powershell", "pwsh"):
            executed = cluster.cli("controller", "exec", "--device", target, "--shell", row["name"], "--script-file", str(script_file))
            assert executed.stdout.strip() == "script-file-ready 中文🌍" and not executed.stderr
    cluster.stop("target")
    cluster.cli("target", "ensure")
    assert json.loads(cluster.cli("target", "config", "get").stdout)["shell"] == selected
    deadline = time.monotonic()+30
    while cluster.cli("controller", "doctor", "--device", target, "--json", check=False).returncode:
        assert time.monotonic()<deadline, "target did not reconnect"
        time.sleep(.1)
    code = "Write-Output persisted-shell" if selected == "powershell" else "printf persisted-shell"
    assert "persisted-shell" in cluster.cli("controller", "exec", "--device", target, "--", code).stdout
    cluster.stop("target")
    config_path = cluster.root/"target/config.json"
    config = json.loads(config_path.read_text(encoding="utf-8"))
    config["shell"] = "sh" if os.name == "nt" else "powershell"
    config_path.write_text(json.dumps(config),encoding="utf-8")
    cluster.cli("target", "ensure")
    deadline = time.monotonic()+30
    while cluster.cli("controller", "doctor", "--device", target, "--json", check=False).returncode:
        assert time.monotonic()<deadline, "target did not reconnect with invalid shell config"
        time.sleep(.1)
    denied = cluster.cli("controller", "exec", "--device", target, "--", "echo must-not-run", check=False)
    assert denied.returncode != 0 and "不支持所选 Shell" in denied.stderr and not denied.stdout,denied
    assert json.loads((cluster.root/"target/jobs.json").read_text(encoding="utf-8"))["jobs"] == {}
    cluster.cli("target", "config", "set", "--shell", "auto")
    assert "recovered" in cluster.cli("controller", "exec", "--device", target, "--", "echo recovered").stdout
    # Guarantee the missing-pwsh branch even on hosts that install it. Only
    # this owned target inherits the restricted PATH; the controller/Node do not.
    shell = shutil.which("powershell.exe" if os.name == "nt" else "sh")
    assert shell, "required platform shell is missing"
    if os.name == "nt":
        restricted_path = str(Path(shell).resolve().parent)
    else:
        restricted = cluster.root / "only-sh"
        restricted.mkdir()
        shutil.copy2(Path(shell).resolve(), restricted / "sh")
        restricted_path = str(restricted)
    cluster.cli("target", "config", "set", "--shell", selected)
    cluster.stop("target")
    original_path = os.environ.get("PATH")
    try:
        os.environ["PATH"] = restricted_path
        cluster.cli("target", "ensure")
    finally:
        if original_path is None:
            os.environ.pop("PATH", None)
        else:
            os.environ["PATH"] = original_path
    deadline = time.monotonic()+30
    while cluster.cli("controller", "doctor", "--device", target, "--json", check=False).returncode:
        assert time.monotonic()<deadline, "restricted target did not reconnect"
        time.sleep(.1)
    subprocess.run(["node", str(root/"tests/helpers/shell-adapter.mjs")], env=env, check=True, timeout=180)
    report = root/"dist/verification/shell-adapter.json"
    missing = json.loads(report.read_text(encoding="utf-8"))
    assert any(row["name"] == "explicit_missing_interpreter_does_not_fallback" for row in missing)
    available += [{**row, "name": "without_pwsh/"+row["name"]} for row in missing]
    report.write_text(json.dumps(available, indent=2), encoding="utf-8")
finally:
    cluster.close()
print("PASS persisted shell, unsupported selection fails closed, local config recovers", flush=True)
