"""Authenticate an npm release, install it, and execute only its own CLI/MCP heads."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import sys

from fixtures.installed_package_heads import run_installed_heads
from fixtures.package_payload import inspect_package, verify_installed_package
from fixtures.signed_cycle_report import SignedCycleReport

ROOT=Path(__file__).resolve().parents[1]
sys.path.insert(0,str(ROOT/'scripts'))
from signed_lifecycle_inputs import REPOSITORY, TAG, check_identity
from release_evidence import check_runtime
from verify_release import attest


def clean_environment():
    return {key:value for key,value in os.environ.items()
            if not key.upper().startswith(('XALLOR_','XR_TEST_','NPM_CONFIG_'))
            and key.upper() not in ('NODE_OPTIONS','NODE_PATH','NPM_TOKEN','NODE_AUTH_TOKEN')}


def npm_entrypoint():
    command=shutil.which('npm.cmd' if os.name=='nt' else 'npm')
    if not command:raise RuntimeError('Trusted npm installation is required')
    path=Path(command).resolve()
    cli=path.parent/'node_modules/npm/bin/npm-cli.js' if os.name=='nt' else path
    if not cli.is_file():raise RuntimeError('Cannot locate npm JavaScript entrypoint')
    return cli


def require_install_profile_absent(cycle):
    created=(cycle.work/'install-profile').exists()
    cycle.data['npm_runtime_profile_created']=created
    if created:
        cycle.data['cleanup_errors'].append('npm lifecycle touched its isolated Runtime profile; resource inspection required')
        raise RuntimeError('npm Runtime resources are unconfirmed; workspace retained')


def install_package(cycle,archive,node,supervisor):
    work=cycle.work;prefix=work/'prefix';prefix.mkdir()
    config=work/'npmrc';config.write_text('',encoding='utf-8')
    global_config=work/'npm-globalrc';global_config.write_text('',encoding='utf-8')
    temporary=work/'npm-temp';temporary.mkdir()
    env=clean_environment()
    for key in list(env):
        if key.upper() in ('GH_TOKEN','GITHUB_TOKEN'):env.pop(key)
    install_profile=work/'install-profile'
    env.update(TMP=str(temporary),TEMP=str(temporary),TMPDIR=str(temporary),
               XALLOR_REMOTE_DATA_DIR=str(install_profile),XALLOR_REMOTE_RELAY_URL='ws://127.0.0.1:1/remote')
    command=[str(supervisor),'--timeout','10m','--',node,str(npm_entrypoint()),'install','--prefix',str(prefix),'--cache',str(work/'npm-cache'),
             '--userconfig',str(config),'--globalconfig',str(global_config),'--registry','https://registry.npmjs.org',
             '--ignore-scripts=false','--no-audit','--no-fund','--omit=dev',str(archive)]
    cycle.execution_started=True
    quiet={'creationflags':subprocess.CREATE_NO_WINDOW} if os.name=='nt' else {}
    with (work/'npm-install.log').open('wb') as log:
        result=subprocess.run(command,env=env,cwd=prefix,stdout=subprocess.PIPE,stderr=log,
                              text=True,encoding='utf-8',errors='replace',timeout=620,**quiet)
    supervision=json.loads(result.stdout)
    cycle.data['npm_supervision']=supervision
    cycle.data['npm_supervisor_sha256']=hashlib.sha256(Path(supervisor).read_bytes()).hexdigest()
    cycle.data['npm_exit_code']=supervision.get('exit_code')
    if result.returncode:raise RuntimeError('npm installation failed; owned workspace/log retained')
    expected_boundary='windows_job' if sys.platform=='win32' else 'linux_process_group'
    if (supervision.get('started') is not True or supervision.get('cleanup_confirmed') is not True
            or supervision.get('timed_out') is not False or supervision.get('exit_code')!=0
            or type(supervision.get('pid')) is not int or supervision['pid']<=0 or supervision.get('boundary')!=expected_boundary):
        raise RuntimeError('npm supervision did not confirm successful installation and boundary settlement')
    require_install_profile_absent(cycle)
    lock=prefix/'package-lock.json'
    cycle.data['dependency_install']={'lock_sha256':hashlib.sha256(lock.read_bytes()).hexdigest(),
        'lifecycle_scripts':True,'registry':'https://registry.npmjs.org',
        'scope':'npm dependency resolution and supervised boundary settlement; not dependency signatures or a security sandbox; deliberate Linux setsid/setpgid escapes are outside this boundary'}
    return prefix/'node_modules/xallor-remote-mcp'


def main():
    parser=argparse.ArgumentParser()
    for name in ('archive','bundle','tag','revision','observer','supervisor','output'):parser.add_argument('--'+name,required=True)
    args=parser.parse_args()
    archive=Path(args.archive).resolve();bundle=Path(args.bundle).resolve();observer=Path(args.observer).resolve()
    output=Path(args.output).resolve();supervisor=Path(args.supervisor).resolve()
    node=shutil.which('node')
    protected={archive,observer,supervisor,Path(sys.executable).resolve()}
    if node:protected.add(Path(node).resolve())
    if output.suffix!='.json' or output in protected or output==bundle or bundle in output.parents:
        parser.error('Report must not overwrite candidate/observer inputs')
    cycle=SignedCycleReport(output);error=None
    cycle.data.update(scope='authenticated npm application payload and installed CLI/MCP native owner acceptance; not global shim resolution or dependency signatures',
                      cryptographic_acceptance=False,source_attestations_verified=False,tag=args.tag,revision=args.revision)
    try:
        if not re.fullmatch(TAG,args.tag) or not re.fullmatch('[0-9a-f]{40}',args.revision):
            raise ValueError('Expected explicit version tag and full commit')
        if any(key.startswith('XR_TEST_') and value for key,value in os.environ.items()):
            raise ValueError('Genuine signed package acceptance rejects test-mode environment')
        platform='windows' if sys.platform=='win32' else 'linux' if sys.platform.startswith('linux') else None
        if platform is None:raise ValueError('Native Windows/Linux required')
        name='xallor-remote.exe' if platform=='windows' else 'xallor-remote'
        for path in (archive,bundle/'manifest.json',bundle/name):
            if not path.is_file() or path.is_symlink():raise ValueError('Original regular candidate inputs required')
            attest(path,REPOSITORY,'refs/tags/'+args.tag,args.revision)
        cycle.data['source_attestations_verified']=True
        raw=(bundle/'manifest.json').read_bytes()
        if len(raw)>65536:raise ValueError('Oversized candidate manifest')
        manifest=json.loads(raw);check_identity(manifest,dict(version=args.tag[1:],revision=args.revision),platform)
        if manifest['artifact']['filename']!=name or (bundle/name).stat().st_size>128*1024*1024:
            raise ValueError('Invalid native candidate')
        check_runtime((bundle/name).read_bytes(),manifest,platform)
        inventory=inspect_package(archive,manifest)
        if inventory['manifest_sha256']!=hashlib.sha256(raw).hexdigest():
            raise ValueError('Packaged manifest bytes differ from the authenticated raw subject')
        cycle.data.update(archive_sha256=hashlib.sha256(archive.read_bytes()).hexdigest(),
                          payload_inventory=inventory,platform=platform)
        if not node:raise RuntimeError('Trusted Node installation required')
        node=str(Path(node).resolve())
        cycle.create_workspace();package=install_package(cycle,archive,node,supervisor)
        entries=verify_installed_package(package,inventory)
        cycle.data['installed_payload_verified']=True
        head_env={key:value for key,value in clean_environment().items() if key.lower()!='path'}
        gh=shutil.which('gh')
        if not gh:raise RuntimeError('Trusted GitHub CLI is required')
        head_env['PATH']=os.pathsep.join(dict.fromkeys([str(Path(node).parent),str(Path(gh).resolve().parent)]))
        run_installed_heads(cycle,entries,node,observer,manifest,raw,head_env)
        verify_installed_package(package,inventory)
        if hashlib.sha256(archive.read_bytes()).hexdigest()!=cycle.data['archive_sha256']:
            raise ValueError('Authenticated archive changed during acceptance')
        cycle.data['passed']=True
    except Exception as caught:error=caught
    finally:
        if cycle.data.get('heads_cleanup_confirmed'):
            try:
                require_install_profile_absent(cycle)
                cycle.data['npm_profile_absent_before_removal']=True
                cycle.remove_workspace()
            except Exception as caught:error=error or caught
        cycle.finish(error)
        cycle.data['cryptographic_acceptance']=cycle.data['passed']
        cycle.write()
    print(json.dumps(dict(passed=cycle.data['passed'],output=str(output),failure=cycle.data.get('failure'),cleanup_errors=cycle.data['cleanup_errors'])))
    return 0 if cycle.data['passed'] else 1


if __name__=='__main__':
    sys.stdout.reconfigure(encoding='utf-8',errors='replace');sys.exit(main())
