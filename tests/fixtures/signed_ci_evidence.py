"""Bind CI success to original candidates, native observations and recovery effects."""
import re

from .restore_evidence import candidate_selection, identity, restored_operation
from .runtime_ready_observation import validate_ready
from runtime_image_observation import REQUIRED_SEALS

CASES = ('first_signed_install', 'missing_baseline_manifest_refused_before_stop',
         'signed_upgrade_preserves_identity_config', 'signed_rollback_preserves_identity_config_files',
         'rehashed_upgrade_refused_without_stopping_current', 'failed_signed_candidate_restores_previous_process')


def native_image(image, digest, platform, entry=None):
    if image.get('loaded_sha256') != digest:
        raise ValueError('Native loaded image differs from authenticated candidate')
    if platform == 'windows':
        if image.get('kernel_image_mapping') is not True or image.get('process_handle_observed') is not True:
            raise ValueError('Missing Windows original image/handle binding')
    elif (image.get('pidfd_observed') is not True or type(image.get('seals')) is not int
          or image['seals'] & REQUIRED_SEALS != REQUIRED_SEALS):
        raise ValueError('Missing Linux sealed image/pidfd binding')
    if entry is not None and image.get('entry') != entry:
        raise ValueError('Wrong native Runtime entry')


def selection(row):
    ready = row['runtime_observation']
    return {key: ready[key] for key in ('sha256', 'activation_id', 'launch_id')}


def check_fault(fault, candidates, baseline, recovered, platform):
    if (any(fault.get(key) is not True for key in ('settled', 'durable_restore_confirmed',
            'candidate_kernel_exit', 'worker_exited', 'database_failure_log_observed_before_unlock'))
            or fault.get('exit_observer') != ('process_handle' if platform == 'windows' else 'pidfd')
            or type(fault.get('installer_exit')) is not int or fault['installer_exit'] == 0):
        raise ValueError('Native failed-start restoration is incomplete')
    a, b = [candidate['manifest'] for candidate in candidates]
    before, admitted, active = fault['baseline_state'], fault['admitted_operation'], fault['candidate_operation']
    binding = active['binding']
    if (admitted.get('outcome') != 'pending' or active.get('outcome') != 'pending'
            or active['request'] != admitted['request'] or active['namespace_sha256'] != admitted['namespace_sha256']
            or not re.fullmatch('[a-f0-9]{32}', fault.get('operation_id', ''))
            or fault['operation_id'] != admitted['request']['id']
            or before.get('current') != a or identity(before, 'current') != selection(baseline)
            or binding.get('manifest') != b or binding.get('clear_only') is not False
            or binding.get('manifest_sha256') != candidates[1]['manifest_sha256']
            or any(binding['baseline'].get(role) != identity(before, role) for role in ('current', 'previous', 'pending'))):
        raise ValueError('Fault transaction is not the admitted A/B operation')
    candidate = candidate_selection(binding, active)
    if candidate is None or candidate != fault['candidate_selection']:
        raise ValueError('Missing actual pending candidate selection')
    target = restored_operation(dict(source=fault['query_source'], settled=fault['settled'], operation=fault['operation']),
                                admitted, binding, candidate, baseline['pid'])
    if target != fault['restoration_target'] or target != selection(recovered):
        raise ValueError('Recovery effects do not identify the newly observed A owner')
    for key in ('worker_pid', 'candidate_pid'):
        if type(fault.get(key)) is not int or fault[key] <= 0:
            raise ValueError('Missing native worker/candidate identity')
    if fault['worker_pid'] == fault['candidate_pid'] or fault['candidate_image'].get('parent_pid') != fault['worker_pid']:
        raise ValueError('Candidate is not bound to its retained worker')
    native_image(fault['worker_image'], a['artifact']['sha256'], platform, 'runtime-transaction')
    native_image(fault['candidate_image'], b['artifact']['sha256'], platform, 'runtime-serve')


def accept_report(report, candidates, observer_sha, platform):
    manifests = [candidate['manifest'] for candidate in candidates]
    if (platform not in ('windows', 'linux') or report.get('passed') is not True or report.get('status') != 'passed'
            or report.get('workspace_removed') is not True or report.get('cleanup_errors') != []
            or 'failure' in report or report.get('versions') != [m['build'] for m in manifests]
            or report.get('installer_sha256') != manifests[1]['artifact']['sha256']
            or report.get('observer_sha256') != observer_sha):
        raise ValueError('Lifecycle did not prove these exact inputs and complete cleanup')
    rows = report.get('process_observations', [])
    if len(rows) != 4:
        raise ValueError('Four separate owner launches are required')
    launches = set()
    for row, index in zip(rows, (0, 1, 0, 0)):
        if (type(row.get('pid')) is not int or row['pid'] <= 0
                or any(row.get(key) is not True for key in ('image_bound', 'kernel_exit', 'reference_closed'))
                or row.get('image_sha256') != manifests[index]['artifact']['sha256']):
            raise ValueError('Owner identity/exit is not confirmed')
        ready = row['runtime_observation']
        installed = dict(current_activation=ready.get('activation_id'), current_launch_id=row.get('launch_id'))
        validate_ready(dict(peer_pid=row['pid'], runtime=ready), installed, manifests[index], row)
        if row['launch_id'] in launches:
            raise ValueError('An owner launch is duplicated across replacement phases')
        launches.add(row['launch_id'])
    results = report.get('results', [])
    if [row.get('name') for row in results] != list(CASES):
        raise ValueError('Lifecycle did not execute all six required cases')
    for case, (result, index, rowindex) in enumerate(zip(results, (0, 0, 1, 0, 0, 0), (0, 0, 1, 2, 2, 3))):
        manifest, row = manifests[index], rows[rowindex]
        running, image = result['running'], result['image_observation']
        if (result.get('status') != 'pass' or running.get('pid') != row['pid']
                or running.get('runtime_owner_protocol') != 1
                or running.get('sha256') != manifest['artifact']['sha256']
                or any(running.get(key) != value for key, value in manifest['build'].items()
                       if key in ('version', 'revision', 'os', 'arch', 'protocol', 'profile_format'))
                or image.get('runtime_observation') != row['runtime_observation']
                or image.get('retained_sha256') != manifest['artifact']['sha256']
                or image.get('manifest_present') is not (case != 1)
                or image.get('ready_binding') != ('prior_bound_reference_missing_manifest_case_only' if case == 1 else 'native_peer_and_owner_launch')):
            raise ValueError('Case is not bound to its authenticated live owner observation')
        native_image(image, manifest['artifact']['sha256'], platform)
    check_fault(report['startup_fault'], candidates, rows[2], rows[3], platform)


def accept_package_report(report, candidate, observer_sha, node_sha, platform, supervisor_sha):
    manifest=candidate['manifest'];digest=manifest['artifact']['sha256']
    archive=next((row for row in candidate['indexed_artifacts'] if row['name']==f"xallor-remote-mcp-{candidate['version']}.tgz"),None)
    if (archive is None or report.get('status')!='passed' or report.get('cleanup_errors')!=[] or 'failure' in report
            or any(report.get(key) is not True for key in ('passed','workspace_removed','cryptographic_acceptance',
                    'source_attestations_verified','installed_payload_verified','heads_complete','heads_cleanup_confirmed'))
            or report.get('tag')!=candidate['tag'] or report.get('revision')!=candidate['revision']
            or report.get('native_sha256')!=digest or report.get('archive_sha256')!=archive['sha256']
            or report.get('observer_sha256')!=observer_sha or report.get('node_sha256')!=node_sha
            or report.get('platform')!=platform or report.get('npm_exit_code')!=0 or report.get('npm_runtime_profile_created') is not False):
        raise ValueError('Installed package acceptance does not bind the authenticated inputs and cleanup')
    if report.get('npm_profile_absent_before_removal') is not True:
        raise ValueError('Missing final npm Runtime-profile absence check')
    supervision=report.get('npm_supervision',{})
    if (report.get('npm_supervisor_sha256')!=supervisor_sha or supervision.get('started') is not True
            or supervision.get('cleanup_confirmed') is not True or supervision.get('timed_out') is not False
            or supervision.get('exit_code')!=0 or type(supervision.get('pid')) is not int or supervision['pid']<=0
            or supervision.get('boundary')!=('windows_job' if platform=='windows' else 'linux_process_group')):
        raise ValueError('npm supervised process boundary is not confirmed settled')
    checks=report.get('cold_start_checks',[])
    if (len(checks)!=2 or [item.get('phase') for item in checks]!=['after_cli_version','after_mcp_initialize']
            or any('running' not in item or item['running'] is not None or item.get('installation_state_absent') is not True for item in checks)):
        raise ValueError('MCP cold start lacks explicit prior owner/state absence checks')
    inventory=report.get('payload_inventory',{})
    if inventory.get('manifest')!=manifest or inventory.get('manifest_sha256')!=candidate['manifest_sha256']:
        raise ValueError('Installed package manifest differs from the original signed subject')
    dependency=report.get('dependency_install',{})
    if not re.fullmatch('[0-9a-f]{64}',dependency.get('lock_sha256','')) or dependency.get('lifecycle_scripts') is not True:
        raise ValueError('Standard npm dependency installation evidence missing')
    head=report.get('mcp_head',{})
    if head.get('reaped') is not True or type(head.get('pid')) is not int or head['pid']<=0 or type(head.get('exit_code')) is not int:
        raise ValueError('Installed MCP child exit not confirmed')
    results=report.get('results',[])
    expected=['installed_cli_version','mcp_cold_start_and_cli_reuse','installed_mcp_reconnect_after_cli_restart']
    if [row.get('name') for row in results]!=expected or any(row.get('passed') is not True for row in results):
        raise ValueError('Installed entrypoint cases are incomplete')
    version=results[0]['build']
    if (version.get('sha256')!=digest or version.get('runtime_owner_protocol')!=1
            or any(version.get(key)!=value for key,value in manifest['build'].items()
                   if key in ('version','revision','dirty','os','arch','protocol','profile_format'))):
        raise ValueError('Installed CLI does not report this authenticated native candidate')
    rows=report.get('process_observations',[])
    if len(rows)!=2 or rows[0].get('launch_id')==rows[1].get('launch_id'):
        raise ValueError('Two distinct Runtime launches are required for installed-head acceptance')
    for result,row in zip(results[1:],rows):
        if (row.get('image_sha256')!=digest or any(row.get(key) is not True for key in
                ('image_bound','kernel_exit','reference_closed')) or type(row.get('pid')) is not int or row['pid']<=0):
            raise ValueError('Installed Runtime native identity/exit is missing')
        ready=row['runtime_observation'];running=result['running'];image=result['image_observation']
        state=dict(current_activation=ready.get('activation_id'),current_launch_id=row.get('launch_id'))
        validate_ready(dict(peer_pid=row['pid'],runtime=ready),state,manifest,running)
        if (running.get('sha256')!=digest or running.get('runtime_owner_protocol')!=1
                or image.get('runtime_observation')!=ready or image.get('manifest_present') is not True
                or image.get('retained_sha256')!=digest or image.get('ready_binding')!='native_peer_and_owner_launch'):
            raise ValueError('Installed head phase is not bound to its ready owner')
        native_image(image,digest,platform)
