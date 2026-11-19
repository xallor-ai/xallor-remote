"""Shared opt-in fixture arguments and evidence for native integration tests."""
import hashlib
import json
from pathlib import Path
from .runtime_cluster import Cluster


def add_mechanics_options(parser, *, add_go=True):
    modes = parser.add_mutually_exclusive_group()
    modes.add_argument("--owner-mechanics", action="store_true",
                       help="Build native _test owner with controlled verifier; not signed acceptance")
    modes.add_argument("--owner-mechanics-binary",
                       help="Explicit trusted precompiled native _test owner for offline mechanics")
    if add_go:
        parser.add_argument("--go", default="go")


def cluster_from_options(args, report_path=None, harness=None):
    if report_path is not None:
        begin_report(report_path, None, harness)
    build = args.owner_mechanics
    prebuilt = args.owner_mechanics_binary
    if build and prebuilt:
        raise ValueError("Select only one owner mechanics source")
    cluster = Cluster(args.binary, mechanics_go=args.go if build else None, mechanics_binary=prebuilt)
    if report_path is not None:
        begin_report(report_path, cluster, harness)
    return cluster


def completed_report(cluster, results):
    if not cluster.cleanup_complete:
        raise RuntimeError("Owned cluster cleanup must complete before a success report")
    if not results or any(not isinstance(row, dict) or row.get("status") != "pass" for row in results):
        raise ValueError("Completed evidence requires nonempty passing scenarios")
    return {"status":"passed", "evidence":dict(cluster.evidence),
            "cleanup_complete":True, "results":results}


def begin_report(path, cluster, harness):
    evidence = cluster.evidence if cluster is not None else {}
    evidence["harness_sha256"] = hashlib.sha256(Path(harness).read_bytes()).hexdigest()
    target = Path(path)
    target.parent.mkdir(parents=True, exist_ok=True)
    target.write_text(json.dumps({"status":"incomplete", "evidence":dict(evidence),
                                  "cleanup_complete":False, "results":[]}, indent=2), encoding="utf-8")
