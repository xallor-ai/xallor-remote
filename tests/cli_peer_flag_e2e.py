"""Real CLI regression for the pre-release peer target flag migration."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import sys
from fixtures.mechanics_cli import add_mechanics_options, cluster_from_options, completed_report

sys.stdout.reconfigure(encoding='utf-8', errors='replace')
ROOT = Path(__file__).resolve().parents[1]


def main():
    parser = argparse.ArgumentParser()
    platform = 'windows-amd64/xallor-remote.exe' if os.name == 'nt' else 'linux-amd64/xallor-remote'
    parser.add_argument('--binary', default=str(ROOT/'dist/release'/platform))
    add_mechanics_options(parser)
    args = parser.parse_args()
    output = ROOT/'dist/verification/cli-peer-flag.json'
    cluster = cluster_from_options(args, output, __file__)
    cluster.url = 'ws://127.0.0.1:1/remote'
    cluster.evidence['binary_sha256'] = hashlib.sha256(cluster.binary.read_bytes()).hexdigest()
    results = []
    # Register before dispatch, including any unexpected legacy parser side effect.
    cluster.profiles.extend(['old', 'new'])
    try:
        old = cluster.cli('old','peer','add','--id','123456789','--grant','xr_grant_test',check=False)
        assert old.returncode != 0 and 'unknown flag' in old.stderr and not (cluster.root/'old').exists(), (old.stdout,old.stderr)
        results.append('legacy_id_rejected_without_initializing_profile')
        new = cluster.cli('new','peer','add','--device','123456789','--grant','xr_grant_test')
        assert 'unknown flag' not in new.stderr and '123456789' in new.stdout, (new.stdout,new.stderr)
        assert (cluster.root/'new').exists(), 'new --device did not reach the command implementation'
        results.append('device_flag_reaches_runtime_owner_and_adds_peer')
    finally:
        cluster.close()
    output.write_text(json.dumps(completed_report(cluster, results),indent=2),encoding='utf-8')
    print('peer --device accepted; legacy --id rejected; strict cleanup complete')


if __name__ == '__main__': main()
