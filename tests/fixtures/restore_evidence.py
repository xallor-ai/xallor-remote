"""Acceptance checks over the same typed/validated lifecycle transaction.

The native observer validates Record shape, request intent and namespace first.
These checks bind that record to this test's A/B inputs and observed processes.
"""
import hashlib
import re


def identity(state, role):
    manifest = state.get(role)
    if manifest is None:
        return {}
    result = {"sha256": manifest["artifact"]["sha256"], "activation_id": state.get(role+"_activation", "")}
    if role != "previous":
        result["launch_id"] = state.get(role+"_launch_id", "")
    return {key: value for key, value in result.items() if value}


def live_binding(observation, before, requested, raw_requested):
    if observation.get("source") != "lifecycle_ipc" or observation.get("settled") is not False:
        raise ValueError("Candidate parent must serve the active lifecycle endpoint")
    record = observation["operation"]
    if record.get("outcome") != "pending":
        raise ValueError("A historical operation cannot identify the active worker")
    binding = record.get("binding")
    if not isinstance(binding, dict):
        return None  # preflight may still be authenticating; only re-observe
    if binding.get("clear_only") is not False or binding.get("manifest") != requested or binding.get("manifest_sha256") != hashlib.sha256(raw_requested).hexdigest():
        raise ValueError("Lifecycle requested bytes differ from expected B")
    for role in ("current", "previous", "pending"):
        if binding["baseline"].get(role) != identity(before, role):
            raise ValueError("Lifecycle baseline differs from retained A selection")
    target = binding.get("requested", {})
    if target.get("sha256") != requested["artifact"]["sha256"] or target.get("launch_id") or not re.fullmatch("[a-f0-9]{32}", target.get("activation_id", "")):
        raise ValueError("Lifecycle requested activation incomplete")
    return binding


def candidate_selection(binding, record):
    """Use an actual worker observation, never Expected/intent or live-file polls."""
    if record.get("outcome") != "pending" or record.get("binding") != binding:
        raise ValueError("Candidate selection is not from the same active transaction")
    effects=record.get("effects", [])
    if not effects:
        return None
    last=effects[-1]
    if last.get("state")=="pending":
        snapshot=last.get("before")  # captured state before this effect, not its intended result
    elif last.get("state") in ("confirmed","not_applied","quiescent_failure"):
        snapshot=last.get("observed")
    else:
        raise ValueError("Uncertain effect cannot establish candidate selection")
    if not isinstance(snapshot,dict):
        raise ValueError("Missing actual selection observation")
    selected=snapshot.get("pending")
    if selected=={}:
        return None
    if snapshot.get("current")!=binding["baseline"]["current"] or snapshot.get("previous")!=binding["baseline"]["previous"]:
        raise ValueError("Candidate startup observation changed its baseline")
    if not isinstance(selected,dict):
        raise ValueError("Invalid observed pending selection")
    if (selected.get("sha256") != binding["requested"]["sha256"] or selected.get("activation_id") != binding["requested"]["activation_id"]
            or not re.fullmatch("[a-f0-9]{32}", selected.get("launch_id", ""))):
        raise ValueError("Pending candidate does not belong to this transaction")
    return dict(selected)


def restored_operation(final, admitted_record, binding, candidate, baseline_pid):
    if final.get("source") != "journal_read_only" or final.get("settled") is not True:
        raise ValueError("Lifecycle recovery has not settled")
    record = final["operation"]
    if (record.get("request") != admitted_record["request"] or record.get("namespace_sha256") != admitted_record["namespace_sha256"]
            or record.get("binding") != binding or record.get("outcome") != "failed"):
        raise ValueError("Recovery changed operation identity/intent/binding")
    effects = record.get("effects", [])
    if not effects or len(effects) > 32 or any(e.get("state") not in ("confirmed", "not_applied", "quiescent_failure") for e in effects):
        raise ValueError("Recovery effects include unresolved work")
    previous = binding["baseline"]
    for effect in effects:
        if effect.get("before") != previous or not isinstance(effect.get("observed"), dict):
            raise ValueError("Recovery effects are not contiguous observations")
        previous = effect["observed"]
    starts = [e for e in effects if e.get("purpose") == "requested" and e.get("name") == "start"]
    if len(starts) != 1 or starts[0].get("state") != "quiescent_failure" or starts[0].get("target") != candidate:
        raise ValueError("Requested B did not have a confirmed quiescent startup failure")
    stops = [e for e in effects if e.get("purpose") == "requested" and e.get("name") == "stop"]
    if (len(stops) != 1 or stops[0].get("state") != "confirmed" or stops[0].get("stop", {}).get("peer_pid") != baseline_pid
            or stops[0]["stop"].get("selection") != binding["baseline"]["current"]
            or stops[0].get("stop_observed", {}).get("process") != "exit_observed"):
        raise ValueError("Baseline A stop is not tied to the observed original process")
    restored = [e for e in effects if e.get("purpose") == "restore"]
    if [e.get("name") for e in restored] != ["start", "commit", "ready"] or any(e.get("state") != "confirmed" for e in restored):
        raise ValueError("Restore start/commit/ready not all confirmed")
    target = {**binding["baseline"]["current"], "launch_id": candidate["launch_id"]}
    if target == binding["baseline"]["current"] or any(e.get("target") != target for e in restored):
        raise ValueError("Restoration is not a new launch of baseline A")
    if effects[-2:] != restored[-2:] or previous.get("current") != target or previous.get("pending") != {}:
        raise ValueError("Final ready selection is not recovered A with no pending candidate")
    return target
