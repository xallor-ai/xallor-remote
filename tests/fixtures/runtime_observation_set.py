"""Retain read-only Runtime identities across an acceptance lifecycle."""
from dataclasses import dataclass
import re
import time

from runtime_image_observation import observe_current_image, open_process_image
from .runtime_ready_observation import validate_ready


@dataclass
class ObservedRuntime:
    running: dict
    evidence: dict
    reference: object
    row: dict


class RuntimeObservations:
    def __init__(self, report, observe_ready):
        self.entries = {}
        self.rows = report.setdefault("process_observations", [])
        self.uncertain = False
        self.observe_ready = observe_ready

    def capture(self, profile, installed, manifest, running, original_manifest):
        try:
            launch = installed.get("current_launch_id")
            if not isinstance(launch, str) or not re.fullmatch("[a-f0-9]{32}", launch):
                raise ValueError("Current Runtime launch identity missing")
            key = (running["pid"], launch)
            if key not in self.entries:
                if original_manifest is None:
                    raise ValueError("Missing-manifest case requires an already bound process reference")
                for (pid, previous_launch), previous in self.entries.items():
                    if pid == running["pid"] and previous_launch != launch and not previous.reference.exited():
                        raise ValueError("A live process cannot acquire another Runtime launch identity")
                reference = open_process_image(running["pid"])
                row = {"pid": running["pid"], "launch_id": launch,
                       "image_sha256": manifest["artifact"]["sha256"], "image_bound": False,
                       "kernel_exit": False, "reference_closed": False}
                observed = ObservedRuntime(dict(running), {}, reference, row)
                self.entries[key] = observed  # register before any fallible image read
                self.rows.append(row)
            observed = self.entries[key]
            if observed.row["image_sha256"] != manifest["artifact"]["sha256"]:
                raise ValueError("A retained Runtime identity changed image")
            evidence = observe_current_image(profile, installed, manifest, running,
                                             original_manifest, process=observed.reference)
            if original_manifest is not None:
                ready = validate_ready(self.observe_ready(), installed, manifest, running)
                if observed.reference.exited():
                    raise ProcessLookupError("Observed Runtime exited during ready binding")
                observed.row["runtime_observation"] = ready
                evidence["ready_binding"] = "native_peer_and_owner_launch"
            else:
                ready = validate_ready({"peer_pid": observed.reference.pid, "runtime": observed.row.get("runtime_observation", {})},
                                       installed, manifest, running)
                evidence["ready_binding"] = "prior_bound_reference_missing_manifest_case_only"
            evidence["runtime_observation"] = ready
            observed.row["image_bound"] = True
            # Keep each phase's evidence independent: a missing-manifest check
            # must not overwrite an earlier phase's manifest-present observation.
            return ObservedRuntime(dict(running), evidence, observed.reference, observed.row)
        except Exception:
            self.uncertain = True
            raise

    def require_exit(self, observed):
        exited = observed.reference.exited()
        observed.row["kernel_exit"] = exited
        if not exited:
            raise RuntimeError("Previous Runtime is still alive after lifecycle transition")

    def close_after_exit(self, timeout=15):
        errors = []
        try:
            deadline = time.monotonic()+timeout
            while True:
                alive = [entry for entry in self.entries.values() if not entry.reference.exited()]
                if not alive:
                    break
                if time.monotonic() >= deadline:
                    raise RuntimeError("Observed Runtime exit not confirmed")
                time.sleep(.05)
        except Exception as error:
            errors.append(str(error))
        finally:
            for entry in self.entries.values():
                try:
                    entry.row["kernel_exit"] = entry.reference.exited()
                    if not entry.row["kernel_exit"]:
                        errors.append("Observed Runtime remains alive")
                except Exception as error:
                    errors.append(str(error))
                try:
                    entry.reference.close()  # never stop/kill on behalf of acceptance
                    entry.row["reference_closed"] = True
                except Exception as error:
                    errors.append(str(error))
        if self.uncertain:
            errors.append("Runtime image observation was incomplete")
        if errors:
            raise RuntimeError("; ".join(errors))


def require_replacement(previous, current):
    if previous.reference is current.reference or previous.row["launch_id"] == current.row["launch_id"]:
        raise RuntimeError("Lifecycle did not create a distinct Runtime launch")
    if not previous.reference.exited():
        raise RuntimeError("Previous Runtime is still alive after replacement")
    if current.reference.exited():
        raise RuntimeError("Replacement Runtime already exited")
