"""Explicit owner/Agent fixture selection for handoff contract tests."""
import hashlib
import os
from pathlib import Path
import shutil
import subprocess

from .mechanics_cli import add_mechanics_options

ROOT = Path(__file__).resolve().parents[2]


def add_handoff_options(parser):
    platform = "windows-amd64/xallor-remote.exe" if os.name == "nt" else "linux-amd64/xallor-remote"
    parser.add_argument("--binary", default=str(ROOT/"dist/release"/platform))
    add_mechanics_options(parser)
    parser.add_argument("--agent-fixture", help="Explicit trusted precompiled Cursor contract test program")


def prepare_agent(cluster, args):
    target = cluster.root / ("agent.exe" if os.name == "nt" else "agent")
    if args.agent_fixture:
        shutil.copy2(Path(args.agent_fixture).resolve(), target)
        target.chmod(0o700)
        source = "supplied precompiled contract fixture; not a real Cursor agent"
    else:
        quiet = {"creationflags":subprocess.CREATE_NO_WINDOW} if os.name == "nt" else {}
        built = subprocess.run([args.go,"build","-o",str(target),"./tests/fixtures/cursor-cli"],
                               cwd=ROOT,capture_output=True,text=True,encoding="utf-8",errors="replace",timeout=180,**quiet)
        if built.returncode:
            raise RuntimeError(f"contract fixture build failed: {built.stdout}\n{built.stderr}")
        source = "go build ./tests/fixtures/cursor-cli; not a real Cursor agent"
    cluster.evidence["agent_fixture_source"] = source
    cluster.evidence["agent_fixture_sha256"] = hashlib.sha256(target.read_bytes()).hexdigest()
    return target
