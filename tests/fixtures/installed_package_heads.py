"""Native owner acceptance through an already authenticated, installed package."""
import hashlib
import json
import os
from pathlib import Path
import subprocess
import time

from .package_stdio import StdioHead
from .runtime_observation_set import RuntimeObservations, require_replacement
from .runtime_ready_observation import query_owner


def require_absent_owner(status, state_path):
    if 'running' not in status or status['running'] is not None or state_path.exists():
        raise ValueError('Cold-start boundary already has an owner or installation state')


def run_installed_heads(cycle, entries, node, observer, manifest, raw_manifest, env):
    report=cycle.data;work=cycle.work;profile=work/'profile';workspace=work/'workspace'
    profile.mkdir();workspace.mkdir()
    (profile/'config.json').write_text(json.dumps(dict(relay_url='ws://127.0.0.1:1/remote',workspace=str(workspace))),encoding='utf-8')
    sentinel=workspace/'keep.txt';sentinel.write_text('package lifecycle 中文',encoding='utf-8')
    env=dict(env,XALLOR_REMOTE_DATA_DIR=str(profile),XALLOR_REMOTE_RELAY_URL='ws://127.0.0.1:1/remote')
    report.update(observer_sha256=hashlib.sha256(observer.read_bytes()).hexdigest(),
                  node_sha256=hashlib.sha256(Path(node).read_bytes()).hexdigest(),
                  native_sha256=manifest['artifact']['sha256'],results=[])
    head=None;service_attempted=False;refs=RuntimeObservations(report,lambda:query_owner(observer,profile,env))
    quiet={'creationflags':subprocess.CREATE_NO_WINDOW} if os.name=='nt' else {}
    def cli(*args):
        cycle.execution_started=True
        try:
            result=subprocess.run([node,str(entries['cli']),*args],env=env,capture_output=True,
                                  text=True,encoding='utf-8',errors='replace',timeout=240,**quiet)
        except subprocess.TimeoutExpired:
            report['cleanup_errors'].append('CLI observation timed out; pending work cannot be inferred absent')
            raise
        if result.returncode:raise RuntimeError('Installed CLI failed: '+result.stderr[-2000:])
        return result.stdout
    def status():return json.loads(cli('runtime','status'))
    def capture():
        running=status()['running']
        if not running:raise ValueError('Installed head did not expose a running Runtime')
        state=json.loads((profile/'runtime/state.json').read_bytes())
        return refs.capture(profile,state,manifest,running,raw_manifest)
    def cold_boundary(phase):
        observed=status()
        try:require_absent_owner(observed,profile/'runtime/state.json')
        except Exception:
            refs.uncertain=True;raise
        report.setdefault('cold_start_checks',[]).append(dict(phase=phase,running=None,installation_state_absent=True))
    def devices():
        result=head.call('tools/call',dict(name='remote.devices',arguments={}))
        if result.get('isError') is not False:raise ValueError('Installed MCP tool failed')
        texts=[x['text'] for x in result.get('content',[]) if x.get('type')=='text']
        if len(texts)!=1:raise ValueError('Expected one structured MCP devices result')
        return json.loads(texts[0])
    def match_devices(value, observed):
        for key in ('pid','sha256','version','revision','protocol','profile_format'):
            if value.get('build',{}).get(key)!=observed.running.get(key):
                raise ValueError('Installed CLI and MCP observe different Runtime '+key)
        for key in ('version','revision','dirty','protocol','profile_format'):
            if value.get('mcp_build',{}).get(key)!=manifest['build'][key]:
                raise ValueError('MCP loaded a different package build '+key)
    try:
        service_attempted=True  # even an unexpected version side effect is scoped and stopped
        version=json.loads(cli('version','--json'))
        report['cli_version_observation']=version
        for key in ('version','revision','dirty','os','arch','protocol','profile_format'):
            if version.get(key)!=manifest['build'][key]:raise ValueError('Installed CLI version differs: '+key)
        if version.get('sha256')!=manifest['artifact']['sha256'] or version.get('runtime_owner_protocol')!=1:
            raise ValueError('Installed native candidate lacks the required authenticated owner identity/protocol')
        report['results'].append(dict(name='installed_cli_version',passed=True,build=version))
        cold_boundary('after_cli_version')
        head=StdioHead([node,str(entries['mcp'])],env,work/'mcp-stderr.log')
        hello=head.call('initialize',dict(protocolVersion='2025-11-25',capabilities={},clientInfo=dict(name='signed-package-acceptance',version='1')),30)
        if hello.get('serverInfo',{}).get('version')!=manifest['build']['version']:
            raise ValueError('Installed MCP initialize reports a different package version')
        head.send('notifications/initialized',{})
        cold_boundary('after_mcp_initialize')
        first_devices=devices();first=capture();match_devices(first_devices,first)
        cli('ensure');reused=capture();match_devices(devices(),reused)
        if reused.reference is not first.reference:raise ValueError('CLI ensure replaced the MCP owner')
        report['results'].append(dict(name='mcp_cold_start_and_cli_reuse',passed=True,
                                     running=first.running,image_observation=first.evidence))
        cli('stop');refs.require_exit(first)
        # A head has one bootstrap attempt. Explicit CLI ensure creates the next
        # owner; the original MCP reconnects without replaying process startup.
        cli('ensure');second_devices=devices();second=capture();match_devices(second_devices,second)
        require_replacement(first,second)
        if second_devices.get('device_id')!=first_devices.get('device_id'):
            raise ValueError('Restart changed the isolated device identity')
        if sentinel.read_text(encoding='utf-8')!='package lifecycle 中文':raise ValueError('User workspace changed')
        report['results'].append(dict(name='installed_mcp_reconnect_after_cli_restart',passed=True,
                                     running=second.running,image_observation=second.evidence))
    finally:
        if head is not None:
            try:
                report['mcp_head']=head.close()
                if head.uncertain:report['cleanup_errors'].append('MCP operation timed out; retain uncertain workspace')
            except Exception as error:report['cleanup_errors'].append(str(error))
        if service_attempted:
            try:
                cli('stop');deadline=time.monotonic()+15
                while status().get('running') is not None:
                    if time.monotonic()>=deadline:raise RuntimeError('Installed Runtime remains running')
                    time.sleep(.1)
            except Exception as error:report['cleanup_errors'].append(str(error))
        try:refs.close_after_exit()
        except Exception as error:report['cleanup_errors'].append(str(error))
        report['heads_cleanup_confirmed']=not report['cleanup_errors']
    if report['cleanup_errors']:raise RuntimeError('Installed package cleanup uncertain; workspace retained')
    if len(report['results'])!=3:raise ValueError('Installed package acceptance is incomplete')
    report['heads_complete']=True
