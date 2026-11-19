"""Standalone runtime verify refusal; install/rollback are explicitly not covered."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile

sys.stdout.reconfigure(encoding='utf-8',errors='replace')
ROOT = Path(__file__).resolve().parents[1]


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--binary')
    parser.add_argument('--go', default='go')
    args = parser.parse_args()
    with tempfile.TemporaryDirectory(prefix='xr-source-gate-') as temporary:
        work = Path(temporary).resolve()
        binary = Path(args.binary).resolve() if args.binary else work/('runtime.exe' if os.name == 'nt' else 'runtime')
        if not args.binary:
            subprocess.run([args.go,'build','-o',str(binary),'./cmd/xallor-remote'],cwd=ROOT,check=True,timeout=180)
        profile = work/'profile'
        profile.mkdir()
        sentinel = profile/'preserved-user-file.txt'
        sentinel.write_text('must survive source verification', encoding='utf-8')
        before = {p.name: hashlib.sha256(p.read_bytes()).hexdigest() for p in profile.iterdir()}
        results = []
        quiet = {'creationflags': subprocess.CREATE_NO_WINDOW} if os.name == 'nt' else {}
        info = json.loads(subprocess.check_output([str(binary),'version','--json'],encoding='utf-8',**quiet))
        bundle = work/'bundle'; bundle.mkdir()
        candidate = bundle/('xallor-remote.exe' if os.name == 'nt' else 'xallor-remote')
        subprocess.run([args.go,'build','-o',str(candidate),'./tests/fixtures/source-candidate'],cwd=ROOT,check=True,timeout=180)
        build = {key:info[key] for key in ('version','revision','protocol','profile_format','os','arch')}
        build['dirty'] = False
        manifest = {'schema':1,'build':build,'artifact':{'filename':candidate.name,
            'size':candidate.stat().st_size,'sha256':hashlib.sha256(candidate.read_bytes()).hexdigest()}}
        manifest_path = bundle/'manifest.json'
        manifest_path.write_text(json.dumps(manifest),encoding='utf-8')
        marker = work/'must-not-execute'
        # Deliberately unavailable verifier, not a fake accepting verifier.
        env = {key: value for key,value in os.environ.items() if not key.upper().startswith('XALLOR_') and key.lower()!='path'}
        env.update(XALLOR_REMOTE_DATA_DIR=str(profile), PATH=str(work/'no-tools'),XALLOR_TEST_CANDIDATE_MARKER=str(marker))
        env.pop('Path',None)

        def refuse(*words):
            result = subprocess.run([str(binary),*words],env=env,capture_output=True,encoding='utf-8',errors='replace',timeout=90,**quiet)
            assert result.returncode != 0 and '未验证' in result.stderr, result.stderr
            assert not marker.exists(), 'untrusted candidate executed before source authentication'
            after = {p.name: hashlib.sha256(p.read_bytes()).hexdigest() for p in profile.iterdir() if p.is_file()}
            assert after == before and all(p.is_file() for p in profile.iterdir()), 'verification changed isolated profile'

        refuse('runtime','verify','--bundle',str(bundle))
        results.append({'name':'verify_refuses_when_system_verifier_unavailable','status':'pass'})
        manifest['build']['dirty'] = True
        manifest_path.write_text(json.dumps(manifest),encoding='utf-8')
        refuse('runtime','verify','--bundle',str(bundle))
        runtime_dir = profile/'runtime'
        assert not (runtime_dir/'versions').exists() and not (runtime_dir/'state.json').exists()
        results.append({'name':'verify_rejects_dirty_identity_without_touching_runtime_or_install_state','status':'pass'})
        manifest['build']['dirty'] = False
        with candidate.open('ab') as f: f.write(b'tampered')
        manifest['artifact'].update(size=candidate.stat().st_size,sha256=hashlib.sha256(candidate.read_bytes()).hexdigest())
        manifest_path.write_text(json.dumps(manifest),encoding='utf-8')
        refuse('runtime','verify','--bundle',str(bundle))
        assert not (runtime_dir/'versions').exists()
        results.append({'name':'verify_does_not_accept_rehashed_manifest_as_source_authentication','status':'pass'})
        output = ROOT/'dist/verification/release-source-gate.json'
        output.parent.mkdir(parents=True,exist_ok=True)
        output.write_text(json.dumps({'scope':'standalone verify negative gates only',
            'running_service_preservation':'not_exercised','profile_unchanged':True,
            'install_upgrade_rollback_vendor_enforcement':'not_integrated','real_product_positive_chain':'not_verified','results':results},indent=2),encoding='utf-8')
        print(json.dumps(results))


if __name__ == '__main__': main()
