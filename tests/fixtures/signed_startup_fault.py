"""Native DB-lock startup fault bound to one real owner transaction and worker."""
from copy import deepcopy
from pathlib import Path
import sqlite3
import subprocess
import sys
import os
import time

from runtime_image_observation import open_process_image, selected_activation
from .lifecycle_observation import notice_id, query_operation, worker_identity
from .restore_evidence import live_binding, candidate_selection, restored_operation
from .runtime_candidate_observation import find_candidate


class StartupFaultUnconfirmed(RuntimeError):
    def __init__(self, message, observation):
        self.observation = observation
        super().__init__(message)


def fail_start_with_database_lock(installer, bundle, profile, env, manifest, *, observer, baseline_state, baseline):
    if sys.platform!="win32" and not sys.platform.startswith("linux"):
        raise StartupFaultUnconfirmed("Native startup fault observation requires Windows or Linux", {})
    profile, bundle = Path(profile).resolve(), Path(bundle).resolve()
    before = deepcopy(baseline_state)
    original = before["current"]
    raw_manifest = (bundle/"manifest.json").read_bytes()
    baseline_image = selected_activation(profile, before, original)
    log_file = profile/"runtime/startup.log"
    log_offset = None
    evidence = {"cause": "identity database temporarily locked", "operation_id": None, "baseline_state": before,
                "worker_binding": "native lifecycle peer plus authenticated baseline image/entry; no historical owner PPID claim"}
    connection = None
    process = worker = candidate = None
    output = error_output = None
    failure = None
    try:
        if baseline.reference.exited():
            raise RuntimeError("Baseline owner exited before fault submission")
        connection = sqlite3.connect(profile/"identity.db", timeout=3)
        connection.execute("BEGIN EXCLUSIVE")
        output = (profile/"runtime/fault-cli.stdout").open("wb")
        error_path = profile/"runtime/fault-cli.stderr"
        error_output = error_path.open("wb")
        quiet={"creationflags":subprocess.CREATE_NO_WINDOW} if os.name=="nt" else {}
        process = subprocess.Popen([str(installer), "runtime", "install", "--bundle", str(bundle)],
                                   env=env, stdout=output, stderr=error_output, **quiet)
        evidence["installer_pid"] = process.pid
        deadline = time.monotonic()+90
        admitted = binding = selected = None
        while time.monotonic() < deadline:
            if log_offset is None and baseline.reference.exited():
                # Exclude diagnostics from the old running A. The successful
                # restore proof later excludes a failed replacement A as cause.
                log_offset = log_file.stat().st_size if log_file.exists() else 0
            if process.poll() is not None:
                raise RuntimeError("Installer ended before the candidate could be independently observed")
            operation_id = notice_id(error_path)
            if operation_id is None:
                time.sleep(.002)
                continue
            evidence["operation_id"] = operation_id
            if worker is None:
                try:
                    first = query_operation(observer, profile, operation_id, bundle, env)
                except RuntimeError:
                    # The listener may not exist yet. Only re-observe this ID;
                    # never submit installation again or infer an owned PID.
                    time.sleep(.01)
                    continue
                peer = worker_identity(first, original)
                worker = open_process_image(peer)
                if worker.exited():
                    raise RuntimeError("Worker exited before held-peer recheck")
                checked = query_operation(observer, profile, operation_id, bundle, env)
                if worker_identity(checked, original) != peer or checked["operation"]["request"] != first["operation"]["request"] or checked["operation"]["namespace_sha256"] != first["operation"]["namespace_sha256"]:
                    raise ValueError("Worker identity changed across retained reference")
                evidence["worker_pid"] = peer
                evidence["worker_image"] = worker.observe(baseline_image, original, entry="runtime-transaction")
                if worker.exited():
                    raise RuntimeError("Worker exited during identity binding")
                admitted = checked["operation"]
                evidence["admitted_operation"] = deepcopy(admitted)
            checked = query_operation(observer, profile, operation_id, bundle, env)
            if worker_identity(checked, original) != worker.pid or worker.exited():
                raise ValueError("Transaction endpoint changed worker")
            binding = live_binding(checked, before, manifest, raw_manifest)
            if binding is None:
                time.sleep(.002)
                continue
            selected = candidate_selection(binding, checked["operation"])
            if selected is None:
                time.sleep(.002)
                continue
            # This path comes from the admitted worker's observed pending
            # selection. Do not open the live state file and impede replacement.
            selected_view={"pending":manifest,"pending_activation":selected["activation_id"]}
            expected = selected_activation(profile, selected_view, manifest, role="pending")
            # The owner can exit while the live transaction query/image checks
            # above are running. Sample again before candidate discovery rather
            # than relying on the next loop iteration (which may never occur).
            if log_offset is None and baseline.reference.exited():
                log_offset = log_file.stat().st_size if log_file.exists() else 0
            candidate,candidate_image = find_candidate(worker,expected,manifest)
            if candidate is not None:
                evidence["candidate_image"] = candidate_image
                evidence["candidate_pid"] = candidate.pid
                evidence["candidate_selection"] = selected
                evidence["candidate_operation"] = deepcopy(checked["operation"])
                evidence["candidate_selection_source"] = "same-operation observed selection, not expected intent"
            if candidate is not None:
                break
            time.sleep(.002)
        if candidate is None or binding is None:
            raise RuntimeError("Did not observe the bound transaction candidate")
        while not candidate.exited():
            if time.monotonic() >= deadline or worker.exited():
                raise RuntimeError("Candidate failure not confirmed before worker completion/deadline")
            time.sleep(.002)
        evidence["candidate_kernel_exit"] = True
        evidence["exit_observer"] = "process_handle" if os.name=="nt" else "pidfd"
        if log_offset is None:
            raise RuntimeError("Baseline exit was not observed before candidate failure diagnostics")
        with log_file.open("rb") as log_stream:
            log_stream.seek(log_offset)
            log = log_stream.read(1024*1024).decode("utf-8", "replace")
        if "SQLITE_BUSY" not in log and "database is locked" not in log:
            raise RuntimeError("No candidate database-lock failure after baseline exit and before unlock")
        evidence["database_failure_log_observed_before_unlock"] = True
        connection.rollback()
        connection.close()
        connection = None
        process.wait(timeout=150)
        if process.returncode == 0:
            raise RuntimeError("Failed candidate reported installation success")
        evidence["installer_exit"] = process.returncode
        final = query_operation(observer, profile, operation_id, bundle, env, journal=True)
        evidence["restoration_target"] = restored_operation(final, admitted, binding, selected, baseline.running["pid"])
        evidence["operation"] = final["operation"]
        evidence["settled"] = final["settled"]
        evidence["query_source"] = final["source"]
        evidence["durable_restore_confirmed"] = True
        if not baseline.reference.exited():
            raise RuntimeError("Original baseline owner has not exited")
        until = time.monotonic()+5
        while not worker.exited():
            if time.monotonic() >= until:
                raise RuntimeError("Transaction worker exit not confirmed")
            time.sleep(.01)
        evidence["worker_exited"] = True
    except BaseException as error:
        failure = error
    finally:
        for reference in (candidate, worker):
            if reference is not None:
                try:
                    reference.close()  # never kill a candidate to manufacture failure
                except Exception as error:
                    failure = failure or error
        if connection is not None:
            try:
                connection.rollback()
            except Exception as error:
                failure = failure or error
            finally:
                try:
                    connection.close()
                except Exception as error:
                    failure = failure or error
        if process is not None and process.poll() is None:
            # Stop only our CLI head on harness failure. An admitted worker is
            # not replayed or killed; its uncertain profile must be retained.
            try:
                process.kill()
                process.wait(timeout=10)
            except Exception as error:
                failure = failure or error
        for stream in (output, error_output):
            if stream is not None:
                try:
                    stream.close()
                except Exception as error:
                    failure = failure or error
    if failure is not None:
        raise StartupFaultUnconfirmed(str(failure), evidence) from failure
    return evidence
