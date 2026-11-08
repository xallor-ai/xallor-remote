"""Real CLI regression for the pre-release peer target flag migration."""
import argparse
import os
from pathlib import Path
import subprocess
import sys
import tempfile

sys.stdout.reconfigure(encoding='utf-8', errors='replace')


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--binary', required=True)
    args = parser.parse_args()
    with tempfile.TemporaryDirectory(prefix='xr-peer-flag-') as temp:
        root = Path(temp).resolve()
        profiles = [root/'old', root/'new']
        env = dict(os.environ,XALLOR_REMOTE_DATA_DIR=str(profiles[0]),XALLOR_REMOTE_RELAY_URL='ws://127.0.0.1:1/remote')
        try:
            old = subprocess.run([args.binary,'peer','add','--id','123456789','--grant','xr_grant_test'],env=env,capture_output=True,encoding='utf-8',errors='replace')
            assert old.returncode != 0 and 'unknown flag' in old.stderr and not (root/'old').exists(), (old.stdout,old.stderr)
            env['XALLOR_REMOTE_DATA_DIR'] = str(profiles[1])
            new = subprocess.run([args.binary,'peer','add','--device','123456789','--grant','xr_grant_test'],env=env,capture_output=True,encoding='utf-8',errors='replace')
            assert new.returncode == 0 and 'unknown flag' not in new.stderr and '123456789' in new.stdout, (new.stdout,new.stderr)
            assert profiles[1].exists(), 'new --device did not reach the command implementation'
        finally:
            for profile in profiles:
                if profile.exists():
                    stop_env = dict(env, XALLOR_REMOTE_DATA_DIR=str(profile))
                    subprocess.run([args.binary,'stop'],env=stop_env,capture_output=True,timeout=20)
    print('peer --device accepted; legacy --id rejected')


if __name__ == '__main__': main()
