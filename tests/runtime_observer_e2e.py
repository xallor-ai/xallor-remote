"""Actual independent Runtime/image/ready observation with a controlled verifier."""
import argparse
import hashlib
import json
from pathlib import Path
import sys

from fixtures.mechanics_cli import add_mechanics_options, cluster_from_options
from fixtures.runtime_observation_set import RuntimeObservations
from fixtures.runtime_ready_observation import query_owner
from fixtures.observed_cluster_cleanup import cleanup_observed_cluster
from runtime_image_observation import selected_activation

ROOT = Path(__file__).resolve().parents[1]


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--observer", required=True)
    parser.add_argument("--binary", default="xallor-remote")
    parser.add_argument("--output", default="dist/verification/runtime-observer.json")
    add_mechanics_options(parser)
    args = parser.parse_args()
    if not args.owner_mechanics and not args.owner_mechanics_binary:
        parser.error("explicit native owner mechanics required")
    output = Path(args.output).resolve()
    protected = [Path(args.observer).resolve(), Path(args.binary).resolve()]
    if args.owner_mechanics_binary:
        protected.append(Path(args.owner_mechanics_binary).resolve())
    if output in protected:
        parser.error("--output must not overwrite observer or Runtime fixture inputs")
    output.parent.mkdir(parents=True, exist_ok=True)
    report = {"passed": False, "scope": "actual native owner/kernel/ready observations with controlled verifier; not signed lifecycle",
              "results": [], "cleanup_errors": [], "cleanup_complete": False}
    output.write_text(json.dumps(report), encoding="utf-8")
    cluster = None
    references = None
    try:
        observer = Path(args.observer).resolve()
        report["observer_sha256"] = hashlib.sha256(observer.read_bytes()).hexdigest()
        cluster = cluster_from_options(args)
        report["evidence"] = cluster.evidence
        cluster.start()
        cluster.add("target")
        profile = cluster.root/"target"
        env = cluster.env("target")
        references = RuntimeObservations(report, lambda: query_owner(observer, profile, env))
        state_path = profile/"runtime/state.json"
        original_state = state_path.read_bytes()
        state = json.loads(original_state)
        manifest = state["current"]
        metadata = selected_activation(profile, state, manifest).parent/"manifest.json"
        original_manifest = metadata.read_bytes()
        def capture(raw_manifest):
            running = json.loads(cluster.cli("target", "runtime", "status").stdout)["running"]
            return references.capture(profile, json.loads(state_path.read_bytes()), manifest, running, raw_manifest)
        def denied_ready():
            try:
                query_owner(observer, profile, env)
            except RuntimeError as error:
                if "complete matching ready identity" not in str(error):
                    raise
            else:
                raise AssertionError("Changed/unverified launch was reported ready")
        first = capture(original_manifest)
        report["initial_image"] = first.evidence
        report["results"].append("native_ready_peer_image_launch_bound")
        changed = dict(state, current_launch_id="e"*32 if state["current_launch_id"] != "e"*32 else "f"*32)
        changed_bytes = json.dumps(changed).encode()
        try:
            state_path.write_bytes(changed_bytes)
            denied_ready()
            assert state_path.read_bytes() == changed_bytes, "Observer repaired selection state"
        finally:
            state_path.write_bytes(original_state)
        report["results"].append("changed_launch_refused_without_state_repair")
        held = metadata.with_name("manifest.observer-test-held")
        metadata.rename(held)
        try:
            prior = capture(None)
            assert prior.reference is first.reference
            assert prior.evidence["ready_binding"] == "prior_bound_reference_missing_manifest_case_only"
            denied_ready()
        finally:
            held.rename(metadata)
        report["results"].append("missing_manifest_only_reuses_prior_binding")
        restored = capture(original_manifest)
        assert restored.reference is first.reference
        assert first.evidence["manifest_present"] and restored.evidence["manifest_present"]
        report["results"].append("restored_selection_uses_same_live_reference")
    except Exception as error:
        report["failure"] = {"type": type(error).__name__, "detail": str(error)}
    finally:
        cleanup_observed_cluster(cluster, references, report)
        if report["cleanup_complete"] and references is not None:
            report["results"].append("native_exit_handles_closed_and_profile_removed")
        report["passed"] = "failure" not in report and not report["cleanup_errors"] and len(report["results"]) == 5
        output.write_text(json.dumps(report, indent=2), encoding="utf-8")
    print(json.dumps({"passed": report["passed"], "output": str(output), "failure": report.get("failure"), "cleanup_errors": report["cleanup_errors"]}))
    return 0 if report["passed"] else 1


if __name__ == "__main__":
    sys.stdout.reconfigure(encoding="utf-8", errors="replace")
    sys.exit(main())
