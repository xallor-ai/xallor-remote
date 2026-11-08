"""CLI/script files/native argv -> IPC -> owned Relay -> target Runtime."""
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
results = []
try:
    fixture = cluster.root/("argv-report.exe" if os.name == "nt" else "argv-report")
    subprocess.run([args.go, "build", "-o", str(fixture), "./tests/fixtures/argv-report"], cwd=root, check=True, timeout=180)
    cluster.start()
    target = cluster.add("target")
    cluster.add("controller")
    cluster.cli("target", "inbound", "on")
    grant = cluster.grant("target")
    cluster.cli("controller", "peer", "add", "--device", target, "--grant", grant)
    workspace = cluster.root/"workspaces/target"
    cwd = workspace/"space ' 中文"
    cwd.mkdir()
    literals = ["", "a b", 'a"b', "a'b", "$HOME", "`", "a&b|c;d", "中文🌍", "C:\\space path\\", "--flag=value",
                "--jsonl", "--jsonl=false", "--unknown-option"]
    direct = cluster.cli("controller", "exec", "--device", target, "--cwd", str(cwd), "--program", str(fixture), "--", *literals)
    data = json.loads(direct.stdout)
    assert data["args"] == literals and os.path.samefile(data["cwd"], cwd), data
    results.append("cli_program_preserves_native_argv_and_target_cwd")
    # Actual remote process argv, not just a parser callback: payload flags must
    # neither enable text-mode JSONL nor disable an explicitly selected JSONL stream.
    structured = cluster.cli("controller", "exec", "--jsonl", "--device", target, "--cwd", str(cwd), "--program", str(fixture), "--", *literals)
    events = [json.loads(line) for line in structured.stdout.splitlines()]
    terminals = [event for event in events if event['type'] in ('exit','error')]
    assert len(terminals) == 1 and events[-1] == terminals[0] and terminals[0]['type'] == 'exit' and terminals[0]['exit_code'] == 0, events
    structured_data = json.loads(''.join(event.get('data','') for event in events if event['type'] == 'stdout'))
    assert structured_data['args'] == literals and os.path.samefile(structured_data['cwd'],cwd), structured_data
    results.append("literal_jsonl_flags_after_separator_reach_real_program_in_text_and_jsonl_modes")
    literal_evidence = {'platform':sys.platform,'status':'pass','expected_args':literals,
                        'text_mode_observed_args':data['args'],'jsonl_mode_observed_args':structured_data['args'],
                        'terminal_type':terminals[0]['type'],'exit_code':terminals[0]['exit_code']}
    request = cluster.root/"execution.json"
    request.write_text(json.dumps({"program":{"executable":str(fixture), "args":literals}, "cwd":str(cwd)}, ensure_ascii=False), encoding="utf-8")
    assert json.loads(cluster.cli("controller", "exec", "--device", target, "--request-file", str(request)).stdout)["args"] == literals
    results.append("json_request_preserves_empty_unicode_and_quoted_arguments")
    literal = "中文 🌍 $HOME ` \"double\" 'single'"
    shell = "powershell" if os.name == "nt" else "sh"
    quoted = "'" + literal.replace("'", "''" if os.name=="nt" else "'\"'\"'") + "'"
    script = ("Write-Output " if os.name=="nt" else "printf '%s\\n' ") + quoted + "\nexit 7"
    local_script = cluster.root/"local script ' 中文.txt"
    local_script.write_text(script, encoding="utf-8-sig")
    before = json.loads(cluster.cli("target", "config", "get").stdout)
    executed = cluster.cli("controller", "exec", "--device", target, "--shell", shell, "--script-file", str(local_script), check=False)
    assert executed.returncode != 0 and literal in executed.stdout and "退出码 7" in executed.stderr, executed
    assert json.loads(cluster.cli("target", "config", "get").stdout) == before
    results.append("utf8_script_file_selects_target_shell_without_mutating_configuration")
    rejected = cluster.cli("controller", "exec", "--device", target, "--", "echo", "must-not-run", check=False)
    assert rejected.returncode != 0 and not rejected.stdout, rejected
    failed = cluster.cli("controller", "exec", "--device", target, "--program", str(fixture), "--", "exit-seven", check=False)
    assert failed.returncode != 0 and "退出码 7" in failed.stderr, failed
    env = dict(cluster.env("controller"), XALLOR_TEST_CLUSTER=str(cluster.root), XALLOR_TEST_TARGET=target, XALLOR_TEST_PROGRAM=str(fixture))
    subprocess.run(["node", str(root/"tests/helpers/execution-input.mjs")], env=env, check=True, timeout=120)
    results.append("ipc_validates_modes_and_preserves_native_exit_without_job_leak")
    results.append("relative_and_absolute_workspaces_programs_scripts_and_cwd_boundary")
    subprocess.run(["node", str(root/"tests/helpers/mcp-release-smoke.mjs")], env=env, check=True, timeout=120)
    results.append("mcp_exec_accepts_script_and_program_contracts")
finally:
    cluster.close()
output = root/"dist/verification/execution-input.json"
output.parent.mkdir(parents=True, exist_ok=True)
output.write_text(json.dumps(results, indent=2), encoding="utf-8")
(output.parent/'cli-literal-arguments.json').write_text(json.dumps(literal_evidence,indent=2,ensure_ascii=False),encoding='utf-8')
print(json.dumps(results), flush=True)
