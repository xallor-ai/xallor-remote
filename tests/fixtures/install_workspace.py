"""Owned installer acceptance workspace; cleanup decisions are explicit."""
import json
from pathlib import Path
import shutil
import tempfile


class InstallWorkspace:
    def __init__(self):
        self.root = Path(tempfile.mkdtemp(prefix="xr-install-source-")).resolve()
        self.profiles = []
        self.cli = None
        (self.root/".owner").write_text("install-source-test", encoding="utf-8")

    def __enter__(self):
        return self

    def __exit__(self, kind, value, traceback):
        errors = []
        for profile in self.profiles:
            try:
                profile = Path(profile)
                if profile.resolve() != profile or not profile.is_relative_to(self.root):
                    raise RuntimeError("Fixture profile is outside owned workspace or redirected")
                if not profile.exists():
                    continue
                if self.cli is None:
                    raise RuntimeError("Fixture cleanup command unavailable")
                stopped = self.cli(profile, "stop")
                if stopped.returncode != 0:
                    raise RuntimeError("Owned Runtime stop returned uncertainty")
                status = self.cli(profile, "runtime", "status")
                if status.returncode != 0 or json.loads(status.stdout)["running"] is not None:
                    raise RuntimeError("Owned Runtime exit observation failed")
            except Exception as error:
                errors.append({"profile": profile.name, "error": str(error)})
        if kind is not None or errors:
            (self.root/"cleanup-report.json").write_text(json.dumps({
                "test_failed": kind is not None, "failure_type": kind.__name__ if kind else None,
                "cleanup_errors": errors, "retained": True,
            }, indent=2), encoding="utf-8")
            if errors:
                raise RuntimeError(f"Owned installer workspace retained at {self.root}; cleanup unconfirmed") from value
            if value is not None and hasattr(value, "add_note"):
                value.add_note(f"Installer acceptance evidence retained at {self.root}")
            return False
        try:
            if self.root.parent != Path(tempfile.gettempdir()).resolve() or not self.root.name.startswith("xr-install-source-"):
                raise RuntimeError("Unexpected installer workspace")
            if self.root.is_symlink() or self.root.resolve() != self.root:
                raise RuntimeError("Installer workspace redirected")
            if (self.root/".owner").read_text(encoding="utf-8") != "install-source-test":
                raise RuntimeError("Installer workspace ownership marker changed")
            if any(p.is_symlink() or getattr(p, "is_junction", lambda: False)() for p in self.root.rglob("*")):
                raise RuntimeError("Installer workspace contains redirected content")
            shutil.rmtree(self.root)
        except Exception as error:
            raise RuntimeError(f"Owned installer workspace cleanup failed; inspect {self.root}") from error
        return False
