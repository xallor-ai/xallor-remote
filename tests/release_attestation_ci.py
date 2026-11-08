"""Negative cryptographic checks; run only after real release CI attestations exist."""
import importlib.util
import os
from pathlib import Path
import subprocess
import tempfile

ROOT = Path(__file__).resolve().parents[1]
spec = importlib.util.spec_from_file_location('verifier',ROOT/'scripts/verify_release.py')
verifier = importlib.util.module_from_spec(spec); spec.loader.exec_module(verifier)
directory = ROOT/'dist/release'
artifact = next(directory.glob('*.zip'))
identity = (os.environ['GITHUB_REPOSITORY'],os.environ['GITHUB_REF'],os.environ['GITHUB_SHA'])


def must_reject(path, workflow='release.yml'):
    try:
        verifier.attest(path,*identity,workflow=workflow)
    except subprocess.CalledProcessError:
        return
    raise AssertionError('Invalid provenance accepted')


with tempfile.TemporaryDirectory(prefix='xr-attestation-') as temporary:
    tampered = Path(temporary)/artifact.name
    tampered.write_bytes(artifact.read_bytes()+b'tampered')
    must_reject(tampered)
must_reject(artifact,workflow='wrong-signer.yml')
print('Real attestation verification rejected tampered bytes and wrong signer')
