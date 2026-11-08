"""Offline checks: importing deployment code must never be needed for validation."""
import importlib.util
from pathlib import Path
import re
import unittest

ROOT = Path(__file__).resolve().parents[1]
spec = importlib.util.spec_from_file_location('relay_deploy_config',ROOT/'scripts/relay_deploy_config.py')
config = importlib.util.module_from_spec(spec); spec.loader.exec_module(config)


class RelayTemplateTests(unittest.TestCase):
    # Every documented public listener must show TLS or both proxy security flags.
    def test_should_require_security_on_public_listener_examples(self):
        files = [ROOT/'README.md']
        for directory in ('docs','deploy','scripts'):
            files.extend(p for p in (ROOT/directory).rglob('*') if p.is_file() and p.suffix in ('.md','.py','.example'))
        for path in files:
            for line in path.read_text(encoding='utf-8').splitlines():
                if re.search(r'relay\s+--listen\s+(?:0\.0\.0\.0:|:)',line):
                    tls = '--tls-cert' in line and '--tls-key' in line
                    proxy = '--reverse-proxy' in line and '--trusted-proxy' in line
                    self.assertTrue(tls or proxy, f'{path}: {line}')

    # Render using explicit gateway networks; no all-network trust or shell input.
    def test_should_render_secure_unit_only_for_explicit_proxy_networks(self):
        unit = config.relay_unit(['192.0.2.10','::ffff:127.0.0.1/128'])
        self.assertIn('--reverse-proxy --trusted-proxy 127.0.0.1',unit)
        self.assertIn('--trusted-proxy 192.0.2.10/32',unit)
        for values in ([],['0.0.0.0/0'],['::/0'],['::ffff:0.0.0.0/96'],['::ffff:127.0.0.1/80'],
                       ['127.0.0.1; touch /tmp/unsafe'],['fe80::1%eth0'],['fe80::1%bad zone'],
                       ['fe80::1%bad\nExecStart=/bin/false'],['127.0.0.1\nRestart=no'],['127.0.0.1 '],['\t127.0.0.1']):
            with self.assertRaises(ValueError): config.relay_unit(values)

    # Preserve original mapped-prefix semantics before masking host bits.
    def test_should_normalize_mapped_prefix_without_accepting_zones_or_controls(self):
        unit = config.relay_unit(['::ffff:127.0.0.1/128','::ffff:127.0.0.25/120','2001:db8::1/128'])
        self.assertIn('--trusted-proxy 127.0.0.0/24',unit)
        self.assertIn('--trusted-proxy 127.0.0.1/32',unit)
        self.assertIn('--trusted-proxy 2001:db8::1/128',unit)
        self.assertEqual(unit.count('ExecStart='),1)

    # The public boundary overwrites incoming spoofed forwarding headers for health and WSS.
    def test_should_overwrite_forwarded_headers_in_gateway_templates(self):
        for name in ('ecs_apply_remote_path.py','ecs_fix_remote_443.py'):
            text = (ROOT/'scripts'/name).read_text(encoding='utf-8')
            for block in re.findall(r'location\s+(?:= /remote/health|\^~ /remote)\s*\{(.*?)\}',text,re.S):
                self.assertIn('X-Forwarded-Proto https;',block)
                self.assertIn('X-Forwarded-For $remote_addr;',block)
                self.assertNotIn('$proxy_add_x_forwarded_for',block)


if __name__ == '__main__': unittest.main()
