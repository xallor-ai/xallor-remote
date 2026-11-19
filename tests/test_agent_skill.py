"""The product Agent Skill must stay within the current non-GUI CLI contract."""

import re
import unittest
from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]
SKILL = ROOT / "skills" / "xallor-remote" / "SKILL.md"


class AgentSkillTests(unittest.TestCase):
    def setUp(self):
        self.text = SKILL.read_text(encoding="utf-8")
        self.normalized = " ".join(self.text.lower().split())

    def test_skill_exists_and_links_current_contracts(self):
        self.assertIn("docs/protocol.md", self.text)
        self.assertIn("Runtime", self.text)
        self.assertIn("--jsonl", self.text)
        self.assertIn("--request-file", self.text)

    def test_skill_does_not_claim_unimplemented_capabilities(self):
        self.assertIn("not yet implemented", self.normalized)
        self.assertIn("sandbox", self.normalized)
        self.assertIn("multi-relay ha", self.normalized)
        self.assertIn("one-time, expiring and long-lived", self.normalized)

    def test_skill_contains_no_bearer_secret_shape(self):
        self.assertIsNone(re.search(r"xr_grant_[A-Za-z0-9]{8,}", self.text))
        self.assertIsNone(re.search(r"(?:device_secret|account_token)\s*[:=]\s*[^<\s]+", self.text, re.I))


if __name__ == "__main__":
    unittest.main()
