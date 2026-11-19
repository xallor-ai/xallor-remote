"""Release provenance must be unavailable to manual branch dry-runs."""

import unittest
from pathlib import Path

import yaml


ROOT = Path(__file__).resolve().parents[1]


class ReleaseWorkflowPolicyTests(unittest.TestCase):
    def test_every_privileged_job_requires_protected_release_tag(self):
        workflow = yaml.safe_load((ROOT / ".github/workflows/release.yml").read_text(encoding="utf-8"))
        required = "github.ref_type == 'tag' && startsWith(github.ref, 'refs/tags/v') && github.ref_protected"
        self.assertEqual(workflow["permissions"], {"contents": "read"})
        privileged = []
        for name, job in workflow["jobs"].items():
            if "write" in job.get("permissions", {}).values():
                privileged.append(name)
                self.assertEqual(job.get("if"), required, name)
        self.assertIn("bundles", privileged)
        self.assertIn("draft", privileged)

    def test_attestation_steps_require_v_tag(self):
        workflow = yaml.safe_load((ROOT / ".github/workflows/release.yml").read_text(encoding="utf-8"))
        steps = workflow["jobs"]["bundles"]["steps"]
        by_name = {step.get("name"): step for step in steps if step.get("name")}
        required = "startsWith(github.ref, 'refs/tags/v')"
        for name in (
            "Sign artifact and SBOM provenance",
            "Verify signed release and reject substitutions",
            "Preserve offline signature bundle",
        ):
            self.assertEqual(by_name[name].get("if"), required, name)

    def test_draft_job_requires_v_tag(self):
        workflow = yaml.safe_load((ROOT / ".github/workflows/release.yml").read_text(encoding="utf-8"))
        self.assertEqual(workflow["jobs"]["draft"].get("if"), "github.ref_type == 'tag' && startsWith(github.ref, 'refs/tags/v') && github.ref_protected")
        self.assertEqual(workflow["jobs"]["draft"].get("environment"), "release")


if __name__ == "__main__":
    unittest.main()
