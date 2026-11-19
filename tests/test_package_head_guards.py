"""Installed-head harnesses must remain scoped even on preparation/absence failures."""
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import patch

import signed_package_e2e as signed
from fixtures.signed_cycle_report import SignedCycleReport
from fixtures import installed_package_heads as heads

ROOT=Path(__file__).resolve().parents[1]


class PackageHeadGuardTests(unittest.TestCase):
    # npm lifecycle hooks receive only an owned Runtime profile and loopback relay.
    def test_npm_environment_binds_owned_profile_and_removes_github_tokens(self):
        with tempfile.TemporaryDirectory() as temporary:
            cycle=SignedCycleReport(Path(temporary)/'report.json');work=cycle.create_workspace()
            supervisor=work/'supervisor';supervisor.write_bytes(b'test tool only')
            def installed(command,**kwargs):
                env=kwargs['env']
                self.assertEqual(Path(env['XALLOR_REMOTE_DATA_DIR']),work/'install-profile')
                self.assertEqual(env['XALLOR_REMOTE_RELAY_URL'],'ws://127.0.0.1:1/remote')
                self.assertNotIn('GH_TOKEN',env);self.assertNotIn('GITHUB_TOKEN',env)
                (work/'prefix/package-lock.json').write_text('{}',encoding='utf-8')
                result=dict(started=True,pid=123,cleanup_confirmed=True,timed_out=False,exit_code=0,
                            boundary='windows_job' if sys.platform=='win32' else 'linux_process_group')
                return subprocess.CompletedProcess(command,0,stdout=json.dumps(result))
            try:
                with patch.dict(os.environ,GH_TOKEN='test-token',GITHUB_TOKEN='test-token'), \
                     patch.object(signed,'npm_entrypoint',return_value=Path('npm-cli.js')), \
                     patch.object(signed.subprocess,'run',side_effect=installed):
                    signed.install_package(cycle,Path('candidate.tgz'),'node',supervisor)
            finally:cycle.remove_workspace();cycle.finish()

    # Existing owner/state and malformed status cannot count as a cold start.
    def test_absence_requires_explicit_null_owner_and_no_installation_state(self):
        with tempfile.TemporaryDirectory() as temporary:
            state=Path(temporary)/'state.json'
            heads.require_absent_owner({'running':None},state)
            for status in ({},{'running':{'pid':123}}):
                with self.assertRaises(ValueError):heads.require_absent_owner(status,state)
            state.write_text('{}',encoding='utf-8')
            with self.assertRaises(ValueError):heads.require_absent_owner({'running':None},state)

    # A late npm hook touching its profile must block final directory reclamation.
    def test_final_install_profile_check_preserves_late_created_state(self):
        with tempfile.TemporaryDirectory() as temporary:
            cycle=SignedCycleReport(Path(temporary)/'report.json');work=cycle.create_workspace()
            try:
                signed.require_install_profile_absent(cycle)
                (work/'install-profile').mkdir()
                with self.assertRaises(RuntimeError):signed.require_install_profile_absent(cycle)
                self.assertTrue((work/'install-profile').is_dir())
                self.assertTrue(cycle.data['cleanup_errors'])
            finally:cycle.remove_workspace();cycle.finish()

    # Report creation must never truncate an explicitly supplied executable input.
    def test_mechanics_report_cannot_overwrite_observer(self):
        with tempfile.TemporaryDirectory() as temporary:
            observer=Path(temporary)/'observer.json';observer.write_bytes(b'keep exact input')
            result=subprocess.run([sys.executable,str(ROOT/'tests/package_heads_mechanics_e2e.py'),
                                   '--observer',str(observer),'--output',str(observer),'--go',str(Path(temporary)/'missing-go')],capture_output=True,timeout=15)
            self.assertNotEqual(result.returncode,0)
            self.assertEqual(observer.read_bytes(),b'keep exact input')


if __name__=='__main__':unittest.main()
