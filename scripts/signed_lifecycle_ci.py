"""Run native signed A/B acceptance with read-only GitHub access; never publish."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import platform as host_platform
import subprocess
import shutil
import sys

from signed_lifecycle_inputs import prepare, request

ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(ROOT/'tests'))
from fixtures.signed_ci_evidence import CASES, accept_report, accept_package_report


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--output-directory', required=True, help='Fresh dedicated directory; never reused')
    args = parser.parse_args()
    output = Path(args.output_directory).resolve()
    output.mkdir(parents=True, exist_ok=False)
    report = dict(status='incomplete', passed=False, stage='request', candidates=[],
                  scope='native signed lifecycle only; not release approval or production deployment',
                  run_id=os.environ.get('GITHUB_RUN_ID'), run_attempt=os.environ.get('GITHUB_RUN_ATTEMPT'))
    summary = output/'acceptance.json'
    def save(): summary.write_text(json.dumps(report, indent=2)+'\n', encoding='utf-8')
    save()
    try:
        expected = request(os.environ)
        platform = 'windows' if sys.platform == 'win32' else 'linux' if sys.platform.startswith('linux') else None
        if platform is None or host_platform.machine().lower() not in ('amd64', 'x86_64'):
            raise ValueError('Native Windows/Linux amd64 runner required')
        head = subprocess.check_output(['git', 'rev-parse', 'HEAD'], cwd=ROOT, text=True).strip()
        if head != expected[1]['revision']:
            raise ValueError('Harness checkout is not the second candidate commit')
        report.update(platform=platform, harness_revision=head, inputs=expected)
        report['stage'] = 'authenticate_candidates';save()
        for label, candidate in zip(('first', 'second'), expected):
            report['candidates'].append(prepare(candidate, output/label, platform));save()
        report['stage'] = 'build_observer';save()
        observer = output/('runtime-observer.exe' if platform == 'windows' else 'runtime-observer')
        with (output/'observer-build.log').open('wb') as log:
            subprocess.run(['go', 'build', '-o', str(observer), './tests/fixtures/runtime-observer'],
                           cwd=ROOT, stdout=log, stderr=subprocess.STDOUT, check=True, timeout=300)
        observer_sha = hashlib.sha256(observer.read_bytes()).hexdigest()
        report['observer_sha256'] = observer_sha
        supervisor=output/('package-supervisor.exe' if platform=='windows' else 'package-supervisor')
        with (output/'package-supervisor-build.log').open('wb') as log:
            subprocess.run(['go','build','-o',str(supervisor),'./tests/fixtures/package-supervisor'],
                           cwd=ROOT,stdout=log,stderr=subprocess.STDOUT,check=True,timeout=300)
        supervisor_sha=hashlib.sha256(supervisor.read_bytes()).hexdigest()
        report['package_supervisor_sha256']=supervisor_sha
        candidates = report['candidates']
        installer = Path(candidates[1]['bundle'])/candidates[1]['manifest']['artifact']['filename']
        report['stage'] = 'native_lifecycle';save()
        command = [sys.executable, str(ROOT/'tests/signed_upgrade_e2e.py'), '--installer', str(installer),
                   '--observer', str(observer), '--first', candidates[0]['bundle'], '--second', candidates[1]['bundle'],
                   '--startup-fault', '--output', str(output/'lifecycle.json')]
        with (output/'lifecycle.log').open('wb') as log:
            result = subprocess.run(command, cwd=ROOT, stdout=log, stderr=subprocess.STDOUT, timeout=2400)
        report['lifecycle_exit_code'] = result.returncode
        if result.returncode:
            raise RuntimeError('Native lifecycle failed; inspect lifecycle.json and lifecycle.log')
        accept_report(json.loads((output/'lifecycle.json').read_bytes()), candidates, observer_sha, platform)
        report['stage']='installed_package';save()
        second=candidates[1]
        archive=Path(second['bundle']).parent/f"xallor-remote-mcp-{second['version']}.tgz"
        node=shutil.which('node')
        if not node:raise RuntimeError('Trusted Node installation is required for installed package acceptance')
        node_sha=hashlib.sha256(Path(node).resolve().read_bytes()).hexdigest()
        command=[sys.executable,str(ROOT/'tests/signed_package_e2e.py'),'--archive',str(archive),
                 '--bundle',second['bundle'],'--tag',second['tag'],'--revision',second['revision'],
                 '--observer',str(observer),'--supervisor',str(supervisor),'--output',str(output/'installed-package.json')]
        with (output/'installed-package.log').open('wb') as log:
            result=subprocess.run(command,cwd=ROOT,stdout=log,stderr=subprocess.STDOUT,timeout=1800)
        report['package_exit_code']=result.returncode
        if result.returncode:raise RuntimeError('Signed installed-package acceptance failed; inspect installed-package evidence')
        accept_package_report(json.loads((output/'installed-package.json').read_bytes()),second,observer_sha,node_sha,platform,supervisor_sha)
        report.update(stage='complete', status='passed', passed=True)
    except Exception as error:
        report.update(status='failed', passed=False, failure=dict(type=type(error).__name__, detail=str(error)))
    finally:
        save()
    print(json.dumps({'passed': report['passed'], 'stage': report['stage'], 'report': str(summary)}))
    return 0 if report['passed'] else 1


if __name__ == '__main__':
    sys.stdout.reconfigure(encoding='utf-8', errors='replace')
    sys.exit(main())
