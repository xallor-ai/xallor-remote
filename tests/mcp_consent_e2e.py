"""Build current MCP and run its exact native stdio consent acceptance case."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile

from fixtures.mcp_consent_evidence import SCENARIOS, validate

ROOT = Path(__file__).resolve().parents[1]


def digest(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def inputs():
    files = [ROOT / name for name in ("go.mod", "go.sum", "internal/buildinfo/version.txt", "internal/buildinfo/profile_format.txt",
                                    "packages/mcp/package.json", "packages/mcp/package-lock.json", "packages/mcp/tsconfig.json")]
    for directory in ("internal", "cmd"):
        files.extend((ROOT / directory).rglob("*.go"))
    for directory in ("src", "scripts", "dist", "node_modules/@modelcontextprotocol", "node_modules/zod", "node_modules/koffi", "node_modules/typescript", "node_modules/@types"):
        files.extend(p for p in (ROOT / "packages/mcp" / directory).rglob("*") if p.is_file())
    return {p.relative_to(ROOT).as_posix(): digest(p) for p in sorted(files)}


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--go", default="go")
    parser.add_argument("--node", default="node")
    parser.add_argument("--output", default="dist/verification/mcp-consent.json")
    args = parser.parse_args()
    output = Path(args.output).resolve()
    output.parent.mkdir(parents=True, exist_ok=True)
    report = {"passed": False, "scope": "independent Runtime and actual stdio MCP consent; controlled native source verifier, no signed-release acceptance"}
    output.write_text(json.dumps(report), encoding="utf-8")
    try:
        node, go = shutil.which(args.node), shutil.which(args.go)
        if not node or not go:
            raise RuntimeError("Node and Go are required, not optional skips")
        env = dict(os.environ, NODE_OPTIONS="", NODE_PATH="", GOMAXPROCS="2", GOFLAGS="", GOWORK="off", GOENV="off", GOTOOLCHAIN="local", GOOS="", GOARCH="")
        report["tools"] = {"node_sha256": digest(Path(node)), "go_sha256": digest(Path(go))}
        build_inputs = {k: v for k, v in inputs().items() if not k.startswith("packages/mcp/dist/")}
        built = subprocess.run([node, str(ROOT / "packages/mcp/scripts/build.mjs")], cwd=ROOT, env=env, capture_output=True, encoding="utf-8", errors="replace", timeout=180)
        build_after = {k: v for k, v in inputs().items() if not k.startswith("packages/mcp/dist/")}
        report["mcp_build"] = {"exit_code": built.returncode, "stdout": built.stdout, "stderr": built.stderr,
                               "inputs_before": build_inputs, "inputs_after": build_after}
        if built.returncode != 0 or build_inputs != build_after:
            raise RuntimeError("MCP build failed or inputs changed during build")
        report["source_before"] = inputs()
        with tempfile.TemporaryDirectory(prefix="xr-mcp-evidence-") as directory:
            env.update(XR_MCP_ACCEPTANCE_NODE=str(Path(node).resolve()), XR_MCP_ACCEPTANCE_ENTRY=str(ROOT / "packages/mcp/dist/index.js"), XR_INDEPENDENT_REPORT_DIR=directory)
            command = [go, "test", "-json", "-count=1", "-timeout=180s", "-run=^TestIndependentMCP_", "./internal/cli"]
            result = subprocess.run(command, cwd=ROOT, env=env, capture_output=True, encoding="utf-8", errors="replace", timeout=300)
            report.update(exit_code=result.returncode, stdout=result.stdout, stderr=result.stderr)
            reports = {case: json.loads((Path(directory) / (case + ".json")).read_text(encoding="utf-8")) for case in SCENARIOS}
            report["runtime_reports"] = reports
        report["report_directory_removed"] = not Path(directory).exists()
        report["source_after"] = inputs()
        if result.returncode != 0 or report["source_before"] != report["source_after"] or not report["report_directory_removed"]:
            raise RuntimeError("native execution or input/cleanup verification failed")
        events = [json.loads(line) for line in result.stdout.splitlines() if line.strip()]
        for case, inner in reports.items():
            validate(events, inner, case)
        report["passed"] = True
    except Exception as error:
        report["error"] = type(error).__name__ + ": " + str(error)
    output.write_text(json.dumps(report, indent=2), encoding="utf-8")
    print(json.dumps({"passed": report["passed"], "report": str(output), "error": report.get("error")}))
    return 0 if report["passed"] else 1


if __name__ == "__main__":
    sys.stdout.reconfigure(encoding="utf-8", errors="replace")
    sys.exit(main())
