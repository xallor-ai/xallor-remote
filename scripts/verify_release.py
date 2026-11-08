"""Verify signed provenance and the release index before using local artifacts.

Requires GitHub CLI with attestation support. Never installs or executes payloads.
"""
import argparse
import hashlib
import json
from pathlib import Path
import re
import subprocess
import sys


def attest(path, repo, source_ref, revision, workflow='release.yml'):
    subprocess.run(['gh','attestation','verify',str(path),'--repo',repo,
                    '--signer-workflow',repo+'/.github/workflows/'+workflow,
                    '--source-ref',source_ref,'--source-digest',revision,
                    '--deny-self-hosted-runners'], check=True, timeout=120)


def verify(directory, repo, source_ref, revision, verifier=attest):
    directory = Path(directory).resolve()
    index_path = directory/'release-index.json'
    # The trusted expected identity is caller-supplied, never read from the index.
    verifier(index_path, repo, source_ref, revision)
    index = json.loads(index_path.read_text(encoding='utf-8'))
    if index['schema'] != 1 or not index['files']:
        raise ValueError('Invalid release index')
    seen = set()
    for entry in index['files']:
        name = entry['name']
        if not re.fullmatch(r'[a-zA-Z0-9][a-zA-Z0-9._-]*', name) or name in seen:
            raise ValueError('Invalid or duplicate artifact name')
        seen.add(name)
        path = directory/name
        if path.is_symlink() or not path.is_file():
            raise ValueError('Artifact must be a regular file')
        if (path.stat().st_size != entry['size'] or
                hashlib.sha256(path.read_bytes()).hexdigest() != entry['sha256']):
            raise ValueError('Artifact integrity mismatch: ' + name)
        verifier(path, repo, source_ref, revision)
    for manifest in index['builds'].values():
        if manifest['build']['revision'] != revision or manifest['build']['dirty']:
            raise ValueError('Release source identity differs')
    print('Verified signed provenance and all indexed artifact hashes')


if __name__ == '__main__':
    sys.stdout.reconfigure(encoding='utf-8', errors='replace')
    p = argparse.ArgumentParser()
    p.add_argument('--directory', required=True)
    p.add_argument('--repo', default='xallor-ai/xallor-remote')
    p.add_argument('--source-ref', required=True)
    p.add_argument('--revision', required=True)
    args = p.parse_args()
    if not re.fullmatch(r'[a-f0-9]{40}', args.revision) or not args.source_ref.startswith('refs/'):
        p.error('Use an expected full Git revision and refs/tags/<version> or refs/heads/<branch>')
    verify(args.directory, args.repo, args.source_ref, args.revision)
