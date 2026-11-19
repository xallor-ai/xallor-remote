"""Real startup failure/restore observations with explicitly controlled provenance."""
import argparse
from copy import deepcopy
import hashlib
import json
from pathlib import Path
import sys

from fixtures.mechanics_cli import add_mechanics_options, cluster_from_options
from fixtures.observed_cluster_cleanup import cleanup_observed_cluster
from fixtures.runtime_observation_set import RuntimeObservations, require_replacement
from fixtures.runtime_ready_observation import query_owner
from fixtures.signed_startup_fault import StartupFaultUnconfirmed, fail_start_with_database_lock
from fixtures.restore_evidence import identity
from fixtures.lifecycle_observation import query_operation
from runtime_image_observation import selected_activation

ROOT=Path(__file__).resolve().parents[1]


def main():
    parser=argparse.ArgumentParser()
    parser.add_argument("--observer",required=True)
    parser.add_argument("--binary",default="xallor-remote")
    parser.add_argument("--output",default="dist/verification/runtime-recovery-observer.json")
    add_mechanics_options(parser)
    args=parser.parse_args()
    if (sys.platform!="win32" and not sys.platform.startswith("linux")) or (not args.owner_mechanics and not args.owner_mechanics_binary):
        parser.error("Windows/Linux and explicit owner mechanics fixture required")
    observer=Path(args.observer).resolve();output=Path(args.output).resolve()
    protected=[observer,Path(args.binary).resolve()]
    if args.owner_mechanics_binary:protected.append(Path(args.owner_mechanics_binary).resolve())
    if output in protected:parser.error("report path must not overwrite executable inputs")
    output.parent.mkdir(parents=True,exist_ok=True)
    report={"passed":False,"scope":"actual lifecycle worker/candidate DB-lock failure and restore; controlled verifier, not genuine signatures",
            "results":[],"cleanup_errors":[],"cleanup_complete":False}
    output.write_text(json.dumps(report),encoding="utf-8")
    cluster=references=None
    try:
        report["observer_sha256"]=hashlib.sha256(observer.read_bytes()).hexdigest()
        cluster=cluster_from_options(args);report["evidence"]=cluster.evidence
        cluster.start();cluster.add("target")
        profile=cluster.root/"target";env=cluster.env("target")
        state_path=profile/"runtime/state.json"
        before=json.loads(state_path.read_bytes());manifest=before["current"]
        raw=(selected_activation(profile,before,manifest).parent/"manifest.json").read_bytes()
        references=RuntimeObservations(report,lambda:query_owner(observer,profile,env))
        def capture():
            state=json.loads(state_path.read_bytes())
            running=json.loads(cluster.cli("target","runtime","status").stdout)["running"]
            return references.capture(profile,state,manifest,running,raw)
        baseline=capture()
        def fingerprint():
            record=cluster.device_state("target")
            values={k:record[k] for k in ("device_id","secret","grant","inbound","active")}
            return hashlib.sha256(json.dumps(values,sort_keys=True).encode()).hexdigest()
        original_identity=fingerprint();config=(profile/"config.json").read_bytes()
        sentinel=cluster.root/"workspaces/target/recovery-sentinel.txt"
        sentinel.write_text("must survive actual recovery 中文",encoding="utf-8")
        bundle=cluster.root/"candidate-B";bundle.mkdir()
        # A real executable overlay creates different bytes/digest without a
        # product test switch. Only the explicit fixture verifier accepts it;
        # the signed harness never manufactures or substitutes a candidate.
        data=cluster.binary.read_bytes()+b"\nXR-RECOVERY-CANDIDATE-B\n"
        candidate=bundle/manifest["artifact"]["filename"]
        candidate.write_bytes(data);candidate.chmod(0o700)
        next_manifest=deepcopy(manifest)
        next_manifest["artifact"].update(size=len(data),sha256=hashlib.sha256(data).hexdigest())
        (bundle/"manifest.json").write_text(json.dumps(next_manifest),encoding="utf-8")
        report["candidate_sha256"]=next_manifest["artifact"]["sha256"]
        fault=fail_start_with_database_lock(cluster.binary,bundle,profile,env,next_manifest,observer=observer,baseline_state=before,baseline=baseline)
        report["fault"]=fault
        recovered=capture();require_replacement(baseline,recovered)
        final_state=json.loads(state_path.read_bytes())
        assert identity(final_state,"current")==fault["restoration_target"] and not final_state.get("pending")
        assert fingerprint()==original_identity and (profile/"config.json").read_bytes()==config
        assert sentinel.read_text(encoding="utf-8")=="must survive actual recovery 中文"
        report["recovered_image"]=recovered.evidence
        report["results"]=["worker_native_peer_image_entry_bound","candidate_parent_image_and_exit_observed",
                           "same_operation_failed_start_and_restore_confirmed","new_baseline_launch_identity_config_files_preserved"]
    except Exception as error:
        report["failure"]={"type":type(error).__name__,"detail":str(error)}
        if isinstance(error,StartupFaultUnconfirmed):
            report["fault_observation"]=error.observation
            report["cleanup_errors"].append("startup fault observation incomplete; retain original-operation evidence")
            operation_id=error.observation.get("operation_id")
            if operation_id:
                try:
                    report["operation_after_failure"]=query_operation(observer,profile,operation_id,bundle,env,journal=True)
                except Exception as query_error:
                    report["operation_after_failure_error"]=type(query_error).__name__
    finally:
        cleanup_observed_cluster(cluster,references,report)
        report["passed"]="failure" not in report and not report["cleanup_errors"] and report["cleanup_complete"] and len(report["results"])==4
        output.write_text(json.dumps(report,indent=2),encoding="utf-8")
    print(json.dumps({"passed":report["passed"],"output":str(output),"failure":report.get("failure"),"cleanup_errors":report["cleanup_errors"]}))
    return 0 if report["passed"] else 1


if __name__=="__main__":
    sys.stdout.reconfigure(encoding="utf-8",errors="replace")
    sys.exit(main())
