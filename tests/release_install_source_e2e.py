"""Real installer source gate: no mock authenticator or production profiles."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile
import time

sys.stdout.reconfigure(encoding='utf-8',errors='replace')
ROOT = Path(__file__).resolve().parents[1]


def main():
    p = argparse.ArgumentParser()
    p.add_argument('--binary',required=True,help='Installer under test, not the candidate inside --signed-bundle')
    p.add_argument('--signed-bundle',help='Optional independently authenticated real release directory')
    p.add_argument('--go',default='go')
    args = p.parse_args()
    binary = Path(args.binary).resolve()
    results = []
    with tempfile.TemporaryDirectory(prefix='xr-install-source-') as temporary:
        work = Path(temporary).resolve()
        profiles = []

        def env(profile,missing_verifier=False):
            value = dict(os.environ,XALLOR_REMOTE_DATA_DIR=str(profile),XALLOR_REMOTE_RELAY_URL='ws://127.0.0.1:1/remote',
                         XALLOR_TEST_CANDIDATE_MARKER=str(work/'must-not-execute'))
            for key in ('XALLOR_REMOTE_BIN','XALLOR_REMOTE_DEVICE_ID','XALLOR_REMOTE_DEVICE_GRANT'):
                value.pop(key,None)
            if missing_verifier:
                value['PATH'] = str(work/'no-tools'); value.pop('Path',None)
            return value

        def cli(profile,*words,missing_verifier=False):
            return subprocess.run([str(binary),*words],env=env(profile,missing_verifier),capture_output=True,
                                  encoding='utf-8',errors='replace',timeout=120)

        try:
            bundle = work/'unsigned'; bundle.mkdir()
            candidate = bundle/('xallor-remote.exe' if os.name == 'nt' else 'xallor-remote')
            subprocess.run([args.go,'build','-o',str(candidate),'./tests/fixtures/source-candidate'],cwd=ROOT,check=True,timeout=180)
            info = json.loads(cli(work/'info','version','--json').stdout)
            build = {key:info[key] for key in ('version','revision','protocol','profile_format','os','arch')}
            build.update(dirty=False,revision='a'*40)
            manifest = {'schema':1,'build':build,'artifact':{'filename':candidate.name,'size':candidate.stat().st_size,
                        'sha256':hashlib.sha256(candidate.read_bytes()).hexdigest()}}
            (bundle/'manifest.json').write_text(json.dumps(manifest),encoding='utf-8')
            profile = work/'untouched'; profiles.append(profile)
            result = cli(profile,'runtime','install','--bundle',str(bundle),missing_verifier=True)
            assert result.returncode != 0, 'unsigned install succeeded'
            assert not (work/'must-not-execute').exists(), 'Check executed unverified candidate'
            assert not profile.exists(), 'authentication refusal wrote into the fresh install profile'
            results.append({'name':'missing_verifier_refuses_before_check_copy_lock_or_profile_write','status':'pass'})

            # Repeat against a rehashed candidate even when the real gh is available.
            with candidate.open('ab') as f: f.write(b'tampered-and-rehashed')
            manifest['artifact'].update(size=candidate.stat().st_size,sha256=hashlib.sha256(candidate.read_bytes()).hexdigest())
            (bundle/'manifest.json').write_text(json.dumps(manifest),encoding='utf-8')
            profile = work/'rehash'; profiles.append(profile)
            result = cli(profile,'runtime','install','--bundle',str(bundle))
            assert result.returncode != 0 and not profile.exists() and not (work/'must-not-execute').exists(), result.stderr
            results.append({'name':'rehashing_unsigned_bytes_cannot_authorize_install_or_execution','status':'pass'})

            if args.signed_bundle:
                original = Path(args.signed_bundle).resolve()
                verified = cli(work/'verify','runtime','verify','--bundle',str(original))
                assert verified.returncode == 0 and json.loads(verified.stdout)['verified'], verified.stderr
                m = json.loads((original/'manifest.json').read_text(encoding='utf-8'))
                tampered = work/'real-release-rehash'; shutil.copytree(original,tampered)
                real_candidate = tampered/m['artifact']['filename']
                with real_candidate.open('ab') as f: f.write(b'real-release-rehash-regression')
                m['artifact'].update(size=real_candidate.stat().st_size,sha256=hashlib.sha256(real_candidate.read_bytes()).hexdigest())
                (tampered/'manifest.json').write_text(json.dumps(m,indent=2)+'\n',encoding='utf-8')
                profile = work/'real-rehash-empty'; profiles.append(profile)
                refused = cli(profile,'runtime','install','--bundle',str(tampered))
                assert refused.returncode != 0 and not profile.exists(), 'real rehashed release installed or wrote profile'
                results.append({'name':'real_signed_release_rehash_refused_without_any_install_directory_write','status':'pass'})
                # Positive evidence is real GitHub provenance, never a fake gh.
                profile = work/'real-authenticated'; profiles.append(profile)
                installed = cli(profile,'runtime','install','--bundle',str(original))
                assert installed.returncode == 0, installed.stderr
                before = cli(profile,'runtime','status'); assert before.returncode == 0, before.stderr
                state = json.loads(before.stdout)
                assert state['running'], state
                original_manifest = json.loads((original/'manifest.json').read_text(encoding='utf-8'))
                assert state['running']['sha256'] == original_manifest['artifact']['sha256']
                pid = state['running']['pid']
                state_file = profile/'runtime/state.json'; state_bytes = state_file.read_bytes()
                versions = sorted(p.name for p in (profile/'runtime/versions').iterdir())
                refused = cli(profile,'runtime','install','--bundle',str(tampered))
                after = json.loads(cli(profile,'runtime','status').stdout)
                assert refused.returncode != 0 and after['running']['pid'] == pid
                assert state_file.read_bytes() == state_bytes and sorted(p.name for p in (profile/'runtime/versions').iterdir()) == versions
                results.append({'name':'real_authenticated_install_succeeds_and_rehash_preserves_old_process_state_and_versions','status':'pass'})
        finally:
            for profile in profiles:
                if profile.exists():
                    cli(profile,'stop')
                    deadline = time.monotonic()+12
                    while True:
                        status = cli(profile,'runtime','status')
                        if status.returncode == 0 and json.loads(status.stdout)['running'] is None: break
                        assert time.monotonic()<deadline, f'Owned Runtime cleanup failed: {profile}'
                        time.sleep(.1)
        output = ROOT/'dist/verification/release-install-source.json'; output.parent.mkdir(parents=True,exist_ok=True)
        output.write_text(json.dumps({'platform':sys.platform,'real_signed_bundle':bool(args.signed_bundle),'results':results},indent=2),encoding='utf-8')
        print(json.dumps(results))


if __name__ == '__main__': main()
