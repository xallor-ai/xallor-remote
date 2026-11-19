"""Own the report and newly created workspace before lifecycle setup begins."""
import json
import os
from pathlib import Path
import shutil
import tempfile


class SignedCycleReport:
    def __init__(self, output):
        self.output = Path(output).resolve()
        self.output.parent.mkdir(parents=True, exist_ok=True)
        self.data = {"passed": False, "status": "incomplete", "results": [], "cleanup_errors": [],
                     "scope": "native signed install/upgrade/rollback plus pre-activation tamper refusal"}
        self.work = None
        self.identity = None
        self.execution_started = False
        self.removed = False
        self.write()

    def write(self):
        self.output.write_text(json.dumps(self.data, ensure_ascii=False, indent=2), encoding="utf-8")

    def create_workspace(self):
        if self.work is not None:
            raise RuntimeError("Signed lifecycle workspace already created")
        self.work = Path(tempfile.mkdtemp(prefix="xr-signed-upgrade-")).resolve()
        self.identity = self.work.stat()
        self.data["owned_workspace"] = str(self.work)
        (self.work/".owner").write_text("signed-upgrade-test", encoding="utf-8")
        return self.work

    def remove_workspace(self):
        work = self.work
        if work is None or self.removed:
            return
        if (work.parent != Path(tempfile.gettempdir()).resolve() or not work.name.startswith("xr-signed-upgrade-")
                or work.is_symlink() or not os.path.samestat(self.identity, work.stat())):
            raise RuntimeError("Signed lifecycle workspace identity changed")
        marker = work/".owner"
        if marker.exists() and marker.read_text(encoding="utf-8") != "signed-upgrade-test":
            raise RuntimeError("Signed lifecycle workspace owner changed")
        shutil.rmtree(work)
        self.removed = not work.exists()
        if not self.removed:
            raise RuntimeError("Signed lifecycle workspace removal incomplete")

    def finish(self, failure=None):
        if failure is not None:
            self.data["failure"] = {"type": type(failure).__name__, "detail": str(failure)}
            self.data["passed"] = False
        # Only setup with no executed command can be removed here. Once a
        # command has run, the lifecycle driver's stop/observation owns cleanup.
        if self.work is not None and not self.execution_started and not self.removed:
            try:
                self.remove_workspace()
            except Exception as error:
                self.data["cleanup_errors"].append(str(error))
        if self.work is not None and not self.removed and not self.data["cleanup_errors"]:
            self.data["cleanup_errors"].append("Signed lifecycle workspace retained; process cleanup not confirmed")
        self.data["workspace_created"] = self.work is not None
        self.data["workspace_removed"] = self.removed
        self.data["passed"] = self.data["passed"] is True and failure is None and not self.data["cleanup_errors"] and self.removed
        self.data["status"] = "passed" if self.data["passed"] else "failed"
        self.write()
