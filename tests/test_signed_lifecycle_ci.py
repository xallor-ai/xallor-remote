"""Counterexamples for CI identity, extraction, and lifecycle result gates."""
from copy import deepcopy
import hashlib
import json
import os
from pathlib import Path
import sys
import tempfile
import unittest
import subprocess
import warnings
from unittest.mock import patch
import zipfile

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / 'scripts'))
import signed_lifecycle_inputs as inputs
import signed_lifecycle_ci as ci


class SignedLifecycleInputsTests(unittest.TestCase):
    def context(self):
        return dict(GITHUB_REPOSITORY='xallor-ai/xallor-remote', GITHUB_EVENT_NAME='workflow_dispatch',
                    GITHUB_REF_TYPE='tag', GITHUB_REF_PROTECTED='true', GITHUB_REF='refs/tags/v0.3.1',
                    GITHUB_SHA='b'*40, FIRST_TAG='v0.3.0', FIRST_REVISION='a'*40,
                    SECOND_TAG='v0.3.1', SECOND_REVISION='b'*40)

    # Acceptance must fail on a branch/unprotected ref, never become an empty green run.
    def test_requires_exact_protected_second_tag_and_commit(self):
        self.assertEqual(inputs.request(self.context())[1]['revision'], 'b'*40)
        for key, value in [('GITHUB_REF_TYPE','branch'), ('GITHUB_REF_PROTECTED','false'),
                           ('GITHUB_REF','refs/tags/v0.3.0'), ('GITHUB_SHA','c'*40),
                           ('GITHUB_REPOSITORY','fork/remote'), ('GITHUB_EVENT_NAME','push')]:
            with self.subTest(key=key):
                env=self.context();env[key]=value
                with self.assertRaises(ValueError): inputs.request(env)

    # Caller-supplied identity is strict and two independent sources are mandatory.
    def test_rejects_reused_or_shell_like_candidate_inputs(self):
        for key,value in [('FIRST_REVISION','b'*40),('FIRST_TAG','v0.3.1'),
                          ('FIRST_TAG','v0.3.0; echo stolen'),('FIRST_REVISION','main')]:
            env=self.context();env[key]=value
            with self.assertRaises(ValueError): inputs.request(env)

    def archive(self, root, extra=None, symlink=False):
        binary=b'fixture native bytes'
        manifest={'schema':1,'build':{'version':'0.3.0','revision':'a'*40,'dirty':False,
                  'os':'windows','arch':'amd64','protocol':1,'profile_format':1},
                  'artifact':{'filename':'xallor-remote.exe','size':len(binary),
                              'sha256':hashlib.sha256(binary).hexdigest()}}
        path=root/'input.zip'
        with zipfile.ZipFile(path,'w') as z:
            info=zipfile.ZipInfo('xallor-remote.exe')
            if symlink: info.create_system=3;info.external_attr=0o120777 << 16
            z.writestr(info,binary);z.writestr('manifest.json',json.dumps(manifest))
            if extra:
                with warnings.catch_warnings():
                    warnings.simplefilter('ignore',UserWarning)
                    z.writestr(extra,b'not allowed')
        return path,manifest

    # Only the two exact regular members can become an executable bundle.
    def test_extracts_exact_bytes_and_refuses_paths_links_or_duplicate_members(self):
        with tempfile.TemporaryDirectory() as directory:
            root=Path(directory);path,manifest=self.archive(root)
            actual=inputs.extract_raw(path,root/'good','windows')
            self.assertEqual(actual,manifest)
            self.assertEqual((root/'good/xallor-remote.exe').read_bytes(),b'fixture native bytes')
            for extra,link in [('../outside',False),('manifest.json',False),(None,True)]:
                path,_=self.archive(root,extra,link)
                with self.assertRaises(ValueError): inputs.extract_raw(path,root/'bad','windows')
                self.assertFalse((root/'bad').exists())

    # Attested metadata must still match the requested release, not merely any signed version.
    def test_rejects_wrong_manifest_source_or_platform(self):
        with tempfile.TemporaryDirectory() as directory:
            _,manifest=self.archive(Path(directory))
            expected=inputs.request(self.context())[0]
            inputs.check_identity(manifest,expected,'windows')
            for key,value in [('version','0.2.6'),('revision','c'*40),('dirty',True),('os','linux')]:
                altered=deepcopy(manifest);altered['build'][key]=value
                with self.assertRaises(ValueError): inputs.check_identity(altered,expected,'windows')


class SignedLifecycleReportTests(unittest.TestCase):
    def fixture(self):
        candidates=[{'manifest':{'build':{'version':'0.3.'+str(i),'revision':letter*40,'protocol':1,'profile_format':1},
                                  'artifact':{'sha256':letter*64}}} for i,letter in enumerate('ab')]
        from test_restore_evidence import RestoreEvidenceTests
        recovery=RestoreEvidenceTests();recovery.setUp()
        recovery.a.clear();recovery.a.update(candidates[0]['manifest'])
        recovery.b.clear();recovery.b.update(candidates[1]['manifest'])
        raw=json.dumps(recovery.b).encode()
        candidates[1]['manifest_sha256']=hashlib.sha256(raw).hexdigest()
        recovery.binding['manifest_sha256']=candidates[1]['manifest_sha256']
        rows=[]
        for index,pid,launch,activation in [(0,100,'e','a'),(1,101,'f','c'),(0,123,'b','a'),(0,124,'d','a')]:
            m=candidates[index]['manifest']
            ready=dict(m['build'],pid=pid,state='ready',sha256=m['artifact']['sha256'],launch_id=launch*32,activation_id=activation*32)
            rows.append(dict(pid=pid,launch_id=launch*32,image_sha256=m['artifact']['sha256'],
                             image_bound=True,kernel_exit=True,reference_closed=True,runtime_observation=ready))
        results=[]
        for case,(name,index,rowindex) in enumerate(zip(ci.CASES,[0,0,1,0,0,0],[0,0,1,2,2,3])):
            m=candidates[index]['manifest']
            ready=deepcopy(rows[rowindex]['runtime_observation'])
            results.append({'name':name,'status':'pass',
                            'running':dict(ready,runtime_owner_protocol=1),
                            'image_observation':dict(loaded_sha256=m['artifact']['sha256'],retained_sha256=m['artifact']['sha256'],
                               kernel_image_mapping=True,process_handle_observed=True,manifest_present=case!=1,
                               ready_binding='prior_bound_reference_missing_manifest_case_only' if case==1 else 'native_peer_and_owner_launch',
                               runtime_observation=ready)})
        report={'passed':True,'status':'passed','workspace_removed':True,'cleanup_errors':[],
                'versions':[c['manifest']['build'] for c in candidates],
                'installer_sha256':'b'*64,'observer_sha256':'c'*64,'results':results,
                'process_observations':rows,
                'startup_fault':{'settled':True,'durable_restore_confirmed':True,
                    'candidate_kernel_exit':True,'worker_exited':True,'exit_observer':'process_handle',
                    'operation_id':'e'*32,'admitted_operation':deepcopy(recovery.admitted),
                    'candidate_operation':dict(deepcopy(recovery.admitted),effects=deepcopy(recovery.final['operation']['effects'][:2])),
                    'baseline_state':deepcopy(recovery.before),'candidate_selection':recovery.target,
                    'restoration_target':dict(recovery.binding['baseline']['current'],launch_id='d'*32),
                    'operation':recovery.final['operation'],'query_source':'journal_read_only',
                    'worker_pid':200,'candidate_pid':201,'installer_exit':72,
                    'worker_image':dict(loaded_sha256='a'*64,entry='runtime-transaction',kernel_image_mapping=True,process_handle_observed=True),
                    'candidate_image':dict(loaded_sha256='b'*64,entry='runtime-serve',parent_pid=200,kernel_image_mapping=True,process_handle_observed=True),
                    'database_failure_log_observed_before_unlock':True}}
        return report,candidates

    # Exit zero or passed=true cannot substitute for exact cases, sources and cleanup.
    def test_rejects_partial_wrong_source_and_unclean_results(self):
        report,candidates=self.fixture()
        ci.accept_report(report,candidates,'c'*64,'windows')
        for key,value in [('results',report['results'][:-1]),('workspace_removed',False),
                          ('installer_sha256','a'*64),('observer_sha256','d'*64),
                          ('process_observations',[]),('cleanup_errors',['still alive'])]:
            altered=deepcopy(report);altered[key]=value
            with self.subTest(key=key),self.assertRaises(ValueError):
                ci.accept_report(altered,candidates,'c'*64,'windows')
        altered=deepcopy(report);altered['startup_fault']['worker_exited']=False
        with self.assertRaises(ValueError): ci.accept_report(altered,candidates,'c'*64,'windows')

    # Removing identity/effect details while retaining all success flags must fail.
    def test_rejects_removed_identity_or_restore_chain(self):
        report,candidates=self.fixture()
        mutations=[lambda r:r['process_observations'][0].pop('pid'),
                   lambda r:r['process_observations'][0].pop('runtime_observation'),
                   lambda r:r['results'][0].pop('image_observation'),
                   lambda r:r['startup_fault'].pop('operation'),
                   lambda r:r['startup_fault']['operation']['effects'].pop(),
                   lambda r:r['startup_fault']['candidate_image'].update(parent_pid=999),
                   lambda r:r['startup_fault'].pop('database_failure_log_observed_before_unlock')]
        for mutate in mutations:
            altered=deepcopy(report);mutate(altered)
            with self.subTest(mutation=mutate),self.assertRaises((ValueError,KeyError)):
                ci.accept_report(altered,candidates,'c'*64,'windows')
        altered=deepcopy(report);altered['results'][2]['running']['revision']='a'*40
        with self.assertRaises(ValueError): ci.accept_report(altered,candidates,'c'*64,'windows')

    # Linux needs sealed native image evidence, not Windows booleans relabeled as Linux.
    def test_linux_requires_pidfd_and_seals(self):
        report,candidates=self.fixture()
        report['startup_fault']['exit_observer']='pidfd'
        with self.assertRaises(ValueError): ci.accept_report(report,candidates,'c'*64,'linux')
        images=[r['image_observation'] for r in report['results']]
        images += [report['startup_fault'][key] for key in ('worker_image','candidate_image')]
        for image in images:
            image.pop('kernel_image_mapping');image.pop('process_handle_observed')
            image.update(pidfd_observed=True,seals=15)
        ci.accept_report(report,candidates,'c'*64,'linux')
        images[-1]['seals']=0
        with self.assertRaises(ValueError): ci.accept_report(report,candidates,'c'*64,'linux')

    # Preparation failure must produce failed evidence without building/executing a candidate.
    def test_runner_records_failed_authentication_before_any_execution(self):
        with tempfile.TemporaryDirectory() as directory:
            output=Path(directory)/'fresh'
            with patch.dict(os.environ,SignedLifecycleInputsTests().context()), \
                 patch.object(sys,'argv',['signed-ci','--output-directory',str(output)]), \
                 patch.object(ci.subprocess,'check_output',return_value='b'*40), \
                 patch.object(ci,'prepare',side_effect=ValueError('signature refused')), \
                 patch.object(ci.subprocess,'run') as run:
                self.assertEqual(ci.main(),1)
                run.assert_not_called()
            report=json.loads((output/'acceptance.json').read_bytes())
            self.assertFalse(report['passed']);self.assertEqual(report['status'],'failed')
            self.assertEqual(report['stage'],'authenticate_candidates')


class SignedLifecycleWorkflowTests(unittest.TestCase):
    # Both native jobs must reach an explicit gate, have read-only tokens, and retain failure evidence.
    def test_workflow_runs_native_matrix_without_publish_authority_or_silent_skip(self):
        import yaml
        root=Path(__file__).resolve().parents[1]
        workflow=yaml.safe_load((root/'.github/workflows/signed-lifecycle.yml').read_text(encoding='utf-8'))
        trigger=workflow.get('on',workflow.get(True))['workflow_dispatch']
        self.assertEqual(set(trigger['inputs']),{'first_tag','first_revision','second_tag','second_revision'})
        self.assertTrue(all(x['required'] and x['type']=='string' for x in trigger['inputs'].values()))
        self.assertEqual(workflow['permissions'],{'contents':'read','attestations':'read'})
        job=workflow['jobs']['native']
        self.assertNotIn('if',job)
        self.assertFalse(job['strategy']['fail-fast'])
        self.assertEqual(set(job['strategy']['matrix']['os']),{'ubuntu-latest','windows-latest'})
        self.assertNotIn('environment',job)
        steps=job['steps']
        self.assertEqual(steps[0]['name'],'Require protected B source')
        self.assertNotIn('if',steps[0])
        env=dict(os.environ,**SignedLifecycleInputsTests().context())
        for key,value,code in [(None,None,0),('GITHUB_REF_PROTECTED','false',1),
                               ('GITHUB_SHA','a'*40,1),('GITHUB_REPOSITORY','fork/remote',1)]:
            current=dict(env)
            if key: current[key]=value
            result=subprocess.run([sys.executable,'-c',steps[0]['run']],env=current,
                                  capture_output=True,timeout=10)
            self.assertEqual(result.returncode,code)
        checkout=next(s for s in steps if s.get('uses','').startswith('actions/checkout@'))
        self.assertEqual(checkout['with']['ref'],'${{ github.sha }}')
        self.assertIs(checkout['with']['persist-credentials'],False)
        run=next(s for s in steps if s.get('name')=='Run signed lifecycle')
        self.assertIn('signed_lifecycle_ci.py',run['run'])
        upload=steps[-1]
        self.assertEqual(upload['if'],'always()')
        self.assertIn('lifecycle.json',upload['with']['path'])
        self.assertEqual(upload['with']['if-no-files-found'],'error')


if __name__ == '__main__': unittest.main()
