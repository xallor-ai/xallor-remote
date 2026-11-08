"""Offline regression tests for artifact binding and fail-closed verification."""
import hashlib
import importlib.util
import io
import json
from pathlib import Path
import subprocess
import tarfile
import tempfile
import unittest
from unittest.mock import patch
import zipfile

ROOT = Path(__file__).resolve().parents[1]


def load(name):
    spec = importlib.util.spec_from_file_location(name, ROOT/'scripts'/f'{name}.py')
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


evidence, verify = load('release_evidence'), load('verify_release')


class ReleaseEvidenceTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix='xr-evidence-')
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name).resolve()
        self.build = {'version':'0.2.0','revision':'a'*40,'dirty':False,'protocol':1,'profile_format':1}

    def artifacts(self, mismatch=False):
        """Tiny two-platform release; a vendor substitution must fail inspection."""
        files = {'package/package.json':json.dumps({'version':'0.2.0'}).encode(),
                 'package/build.json':json.dumps(self.build).encode()}
        for platform, vendor in [('windows','win32-x64'),('linux','linux-x64')]:
            binary = b'fixture-runtime-'+platform.encode()
            name = 'xallor-remote'+('.exe' if platform == 'windows' else '')
            manifest = {'schema':1,'build':dict(self.build,os=platform,arch='amd64'),
                        'artifact':{'filename':name,'size':len(binary),'sha256':evidence.sha(binary)}}
            archive = self.root/f'xallor-remote-0.2.0-{platform}-amd64.zip'
            with zipfile.ZipFile(archive,'w') as z:
                z.writestr(name,binary); z.writestr('manifest.json',json.dumps(manifest))
            Path(str(archive)+'.sha256').write_text(evidence.sha(archive.read_bytes())+'  '+archive.name+'\n',encoding='utf-8')
            files[f'package/vendor/{vendor}/{name}'] = binary + (b'tamper' if mismatch else b'')
            files[f'package/vendor/{vendor}/manifest.json'] = json.dumps(manifest).encode()
        with tarfile.open(self.root/'xallor-remote-mcp-0.2.0.tgz','w:gz') as tar:
            for name,data in files.items():
                entry = tarfile.TarInfo(name); entry.size = len(data); tar.addfile(entry,io.BytesIO(data))

    # Valid zip/vendor identities bind both platforms and the npm build metadata.
    def test_should_accept_matching_zip_and_vendor_manifests(self):
        self.artifacts()
        archives, manifests = evidence.inspect_artifacts(self.root)
        self.assertEqual(len(archives),3); self.assertEqual(set(manifests),{'windows','linux'})

    # A tampered npm vendor cannot pass even when its parent tarball is readable.
    def test_should_reject_substituted_vendor(self):
        self.artifacts(mismatch=True)
        with self.assertRaises(ValueError): evidence.inspect_artifacts(self.root)

    # Checksums are checked against the actual archive, not only the embedded binary.
    def test_should_reject_changed_archive_checksum(self):
        self.artifacts()
        archive = next(self.root.glob('*.zip')); archive.write_bytes(archive.read_bytes()+b'tampered')
        with self.assertRaises(ValueError): evidence.inspect_artifacts(self.root)

    def index(self):
        artifact = self.root/'runtime.zip'; artifact.write_bytes(b'fixture')
        data = {'schema':1,'builds':{'windows':{'build':self.build}},
                'files':[{'name':artifact.name,'size':7,'sha256':hashlib.sha256(b'fixture').hexdigest()}]}
        index = self.root/'release-index.json'; index.write_text(json.dumps(data),encoding='utf-8')
        return artifact,index,data

    # Any failed signature verification stops before artifact handling.
    def test_should_stop_immediately_when_signature_verifier_rejects(self):
        artifact,index,_ = self.index()
        calls = []
        def deny(path,*identity):
            calls.append(path); raise subprocess.CalledProcessError(1,['gh'])
        with self.assertRaises(subprocess.CalledProcessError):
            verify.verify(self.root,'owner/repo','refs/tags/v0.2.0','a'*40,deny)
        self.assertEqual(calls,[index]); self.assertEqual(artifact.read_bytes(),b'fixture')

    # Even a mocked valid index attestation cannot hide an altered artifact hash.
    def test_should_reject_tampering_before_artifact_verification(self):
        artifact,_,_ = self.index(); artifact.write_bytes(b'changed')
        with self.assertRaises(ValueError):
            verify.verify(self.root,'owner/repo','refs/tags/v0.2.0','a'*40,lambda *args:None)

    # A signed index cannot name files outside the supplied release directory.
    def test_should_reject_traversal_in_index(self):
        _,index,data = self.index(); data['files'][0]['name'] = '../escape'
        index.write_text(json.dumps(data),encoding='utf-8')
        with self.assertRaises(ValueError):
            verify.verify(self.root,'owner/repo','refs/tags/v0.2.0','a'*40,lambda *args:None)

    # Expected repository, workflow, ref and commit are enforced by real gh arguments.
    def test_should_bind_verifier_to_expected_signer_and_source(self):
        with patch.object(verify.subprocess,'run') as run:
            verify.attest(Path('artifact.zip'),'owner/repo','refs/tags/v0.2.0','a'*40)
        args = run.call_args.args[0]
        self.assertIn('owner/repo/.github/workflows/release.yml',args)
        self.assertIn('--source-ref',args); self.assertIn('--source-digest',args)
        self.assertIn('--deny-self-hosted-runners',args)
        self.assertTrue(run.call_args.kwargs['check'])


if __name__ == '__main__': unittest.main()
