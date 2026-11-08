"""Guard bilingual onboarding, current CLI examples, and local documentation links."""
from pathlib import Path
import re
import unittest

ROOT = Path(__file__).resolve().parents[1]


def code_examples(text):
    # Blank layout lines do not change commands; languages and all other bytes do.
    return [(language, '\n'.join(line for line in body.splitlines() if line.strip()))
            for language, body in re.findall(r'^```([^\n]*)\n(.*?)^```\s*$', text, re.M | re.S)]


def document_targets(text):
    return {target.split('#', 1)[0] for target in re.findall(r'\]\(([^)]+)\)', text)
            if not re.match(r'[a-zA-Z]+://', target) and not target.startswith('#')}


class ReadmeDocsTests(unittest.TestCase):
    # A missing English entry must fail the gate instead of passing a CN-only audit.
    def test_should_require_both_readmes_and_navigation_from_the_index(self):
        for name in ('README.md', 'README.en.md'):
            self.assertTrue((ROOT/name).is_file(), name)
            for entry in ('docs/README.md', 'docs/cli.md'):
                self.assertIn('../'+name, document_targets((ROOT/entry).read_text(encoding='utf-8')))
        self.assertIn('README.en.md', document_targets((ROOT/'README.md').read_text(encoding='utf-8')))
        self.assertIn('README.md', document_targets((ROOT/'README.en.md').read_text(encoding='utf-8')))

    # Both user paths and Agent request/output examples must use identical commands.
    def test_should_keep_all_executable_examples_identical_between_languages(self):
        chinese = code_examples((ROOT/'README.md').read_text(encoding='utf-8'))
        english = code_examples((ROOT/'README.en.md').read_text(encoding='utf-8'))
        self.assertTrue(chinese)
        self.assertEqual(chinese, english)

    # A future flag translation/rename must be detected, even with valid Markdown.
    def test_should_detect_command_drift_in_an_english_example(self):
        chinese = (ROOT/'README.md').read_text(encoding='utf-8')
        english = (ROOT/'README.en.md').read_text(encoding='utf-8')
        changed = english.replace('peer add --device <device-id>', 'peer add --id <device-id>')
        self.assertNotEqual(code_examples(chinese), code_examples(changed))

    # Onboarding must link to the same owning specs, not separate drifting contracts.
    def test_should_use_matching_existing_cli_relay_mcp_and_release_documents(self):
        targets = []
        for name in ('README.md', 'README.en.md'):
            text = (ROOT/name).read_text(encoding='utf-8')
            links = document_targets(text)
            for target in links:
                self.assertTrue((ROOT/target).is_file(), f'{name}: {target}')
            specs = {target for target in links if target.startswith('docs/')}
            for required in ('docs/cli.md', 'docs/relay.md', 'docs/mcp.md', 'docs/protocol.md',
                             'docs/release-security.md', 'docs/cli-ux-review.md'):
                self.assertIn(required, specs)
            targets.append(specs)
        self.assertEqual(*targets)

    # Preserve the implemented CLI and transport boundary in both language paths.
    def test_should_document_agent_and_user_paths_without_proposed_flags(self):
        for name in ('README.md', 'README.en.md'):
            text = (ROOT/name).read_text(encoding='utf-8')
            for required in ('grant issue', 'grant show', 'peer add --device', 'exec --device',
                             '--request-file ./execution.json --jsonl', 'status --json',
                             '--tls-cert <PEM> --tls-key <PEM>', '127.0.0.1:8443',
                             'XALLOR_REMOTE_DATA_DIR', '#60', '#73', 'UTF-16'):
                self.assertIn(required, text, name)
            for proposed in ('--enable-inbound', 'peer add --id', '--timeout 30s', 'TLS_INSECURE'):
                self.assertNotIn(proposed, text, name)

    # Documentation must not reintroduce trailing spaces/tabs on either OS.
    def test_should_keep_readmes_and_cli_documentation_free_of_trailing_whitespace(self):
        for name in ('README.md', 'README.en.md', 'docs/clients.md', 'docs/cli.md'):
            for number, line in enumerate((ROOT/name).read_text(encoding='utf-8').splitlines(), 1):
                self.assertEqual(line, line.rstrip(' \t'), f'{name}:{number}')


if __name__ == '__main__':
    unittest.main()
