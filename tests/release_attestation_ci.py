"""Negative cryptographic checks; run only after real release CI attestations exist."""
import importlib.util
import os
from pathlib import Path
import subprocess
import tempfile
import json

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

# Check actual raw subjects, not only an archive. An unrelated gh/network error
# cannot satisfy an identity-policy rejection: require the explicit mismatch.
results = []
for platform, binary in (('windows-amd64', 'xallor-remote.exe'), ('linux-amd64', 'xallor-remote')):
    for name in ('manifest.json', binary):
        subject = directory/platform/name
        expected_ref, expected_commit = identity[1:]
        base = ['gh', 'attestation', 'verify', str(subject), '--repo', identity[0],
                '--signer-workflow', identity[0]+'/.github/workflows/release.yml',
                '--cert-oidc-issuer', 'https://token.actions.githubusercontent.com',
                '--predicate-type', 'https://slsa.dev/provenance/v1',
                '--deny-self-hosted-runners', '--format', 'json']
        for case, ref, commit, diagnostic in (
            ('valid', expected_ref, expected_commit, None),
            ('wrong-ref', 'refs/tags/v0.0.0-negative-probe', expected_commit, 'expected SourceRepositoryRef'),
            ('wrong-commit', expected_ref, '0'*40, 'expected SourceRepositoryDigest'),
        ):
            result = subprocess.run(base+['--source-ref', ref, '--source-digest', commit],
                                    capture_output=True, text=True, encoding='utf-8',
                                    errors='replace', timeout=120)
            passed = result.returncode == 0 if diagnostic is None else (
                result.returncode != 0 and diagnostic in result.stderr)
            results.append({'platform': platform, 'subject': name, 'case': case,
                            'exit_code': result.returncode, 'passed': passed,
                            'diagnostic': result.stderr[-2000:]})
            evidence = ROOT/'dist/verification/raw-attestation-policy.json'
            evidence.parent.mkdir(parents=True, exist_ok=True)
            evidence.write_text(json.dumps(results, indent=2), encoding='utf-8')
            if not passed:
                raise AssertionError(f'{platform}/{name} {case}: {result.stderr}')
print('Real raw binary/manifest provenance passed 12 identity-policy checks')
