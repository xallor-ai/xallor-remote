"""The CI package gate cannot substitute raw lifecycle or layout-only results."""
from copy import deepcopy
import hashlib
import json
import unittest

from fixtures.signed_ci_evidence import accept_package_report


class SignedPackageGateTests(unittest.TestCase):
    def fixture(self):
        from test_signed_lifecycle_ci import SignedLifecycleReportTests
        source,candidates=SignedLifecycleReportTests().fixture()
        candidate=candidates[0]
        candidate.update(tag='v0.3.0',revision='a'*40,version='0.3.0',
                         manifest_sha256=hashlib.sha256(json.dumps(candidate['manifest']).encode()).hexdigest(),
                         indexed_artifacts=[{'name':'xallor-remote-mcp-0.3.0.tgz','sha256':'d'*64}])
        first=deepcopy(source['results'][0]);first['name']='mcp_cold_start_and_cli_reuse';first['passed']=True
        second=deepcopy(source['results'][-1]);second['name']='installed_mcp_reconnect_after_cli_restart';second['passed']=True
        version=deepcopy(first['running'])
        report=dict(passed=True,status='passed',workspace_removed=True,cleanup_errors=[],platform='windows',
            cryptographic_acceptance=True,source_attestations_verified=True,installed_payload_verified=True,
            tag=candidate['tag'],revision=candidate['revision'],native_sha256='a'*64,archive_sha256='d'*64,
            observer_sha256='c'*64,node_sha256='e'*64,heads_complete=True,heads_cleanup_confirmed=True,npm_exit_code=0,npm_runtime_profile_created=False,npm_profile_absent_before_removal=True,
            cold_start_checks=[dict(phase=phase,running=None,installation_state_absent=True) for phase in ('after_cli_version','after_mcp_initialize')],
            dependency_install={'lock_sha256':'f'*64,'lifecycle_scripts':True},
            npm_supervisor_sha256='9'*64,npm_supervision=dict(started=True,cleanup_confirmed=True,timed_out=False,exit_code=0,pid=400,boundary='windows_job'),
            payload_inventory={'manifest':candidate['manifest'],'manifest_sha256':candidate['manifest_sha256']},
            process_observations=[source['process_observations'][0],source['process_observations'][3]],
            mcp_head={'pid':300,'reaped':True,'exit_code':0},
            results=[{'name':'installed_cli_version','passed':True,'build':version},first,second])
        return report,candidate

    # A complete installed-head record is separate from the six-case raw installer record.
    def test_accepts_complete_bound_package_evidence(self):
        report,candidate=self.fixture();accept_package_report(report,candidate,'c'*64,'e'*64,'windows','9'*64)

    # Flags must not conceal missing identity, a different npm archive or surviving head.
    def test_rejects_layout_only_changed_source_or_missing_exit(self):
        report,candidate=self.fixture()
        changes=[lambda r:r.update(results=[]),lambda r:r.update(archive_sha256='0'*64),
                 lambda r:r.update(source_attestations_verified=False),
                 lambda r:r['mcp_head'].update(reaped=False),lambda r:r['process_observations'][0].pop('runtime_observation'),
                 lambda r:r['results'][1].pop('image_observation'),lambda r:r.update(workspace_removed=False),lambda r:r.pop('cold_start_checks'),
                 lambda r:r['npm_supervision'].update(cleanup_confirmed=False)]
        for change in changes:
            altered=deepcopy(report);change(altered)
            with self.subTest(change=change),self.assertRaises((ValueError,KeyError)):
                accept_package_report(altered,candidate,'c'*64,'e'*64,'windows','9'*64)


if __name__=='__main__':unittest.main()
