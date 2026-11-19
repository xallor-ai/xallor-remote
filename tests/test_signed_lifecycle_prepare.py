"""Synthetic packaging/verification-boundary tests; these do not verify signatures."""
import hashlib
import io
import json
from pathlib import Path
import shutil
import subprocess
import sys
import tarfile
import tempfile
import unittest
from unittest.mock import patch
import zipfile

sys.path.insert(0,str(Path(__file__).resolve().parents[1]/'scripts'))
import signed_lifecycle_inputs as inputs
import verify_release


class SignedCandidatePreparationTests(unittest.TestCase):
    def setUp(self):
        temp=tempfile.TemporaryDirectory();self.addCleanup(temp.cleanup)
        self.root=Path(temp.name);self.source=self.root/'source';self.source.mkdir()
        self.expected=dict(tag='v0.3.0',version='0.3.0',revision='a'*40)
        build=dict(version='0.3.0',revision='a'*40,dirty=False,protocol=1,profile_format=1)
        files={'package/package.json':json.dumps({'version':'0.3.0'}).encode(),
               'package/build.json':json.dumps(build).encode()}
        manifests={}
        for platform,vendor in [('windows','win32-x64'),('linux','linux-x64')]:
            name='xallor-remote'+('.exe' if platform=='windows' else '')
            binary=b'non-executable-fixture-'+platform.encode()
            manifest={'schema':1,'build':dict(build,os=platform,arch='amd64'),
                      'artifact':{'filename':name,'size':len(binary),'sha256':hashlib.sha256(binary).hexdigest()}}
            raw=json.dumps(manifest).encode();manifests[platform]=manifest
            archive=self.source/f'xallor-remote-0.3.0-{platform}-amd64.zip'
            with zipfile.ZipFile(archive,'w') as z:
                z.writestr(name,binary);z.writestr('manifest.json',raw)
            Path(str(archive)+'.sha256').write_text(hashlib.sha256(archive.read_bytes()).hexdigest()+'  '+archive.name,encoding='utf-8')
            files[f'package/vendor/{vendor}/{name}']=binary
            files[f'package/vendor/{vendor}/manifest.json']=raw
        with tarfile.open(self.source/'xallor-remote-mcp-0.3.0.tgz','w:gz') as tar:
            for name,data in files.items():
                info=tarfile.TarInfo(name);info.size=len(data);tar.addfile(info,io.BytesIO(data))
        for name in ('runtime-windows-amd64.cdx.json','runtime-linux-amd64.cdx.json','mcp.cdx.json'):
            (self.source/name).write_text('{}',encoding='utf-8')
        self.index={'schema':1,'builds':manifests,'files':[
            dict(name=p.name,size=p.stat().st_size,sha256=hashlib.sha256(p.read_bytes()).hexdigest())
            for p in sorted(self.source.iterdir())]}
        self.calls=[]

    def run_prepare(self,denied=None):
        (self.source/'release-index.json').write_text(json.dumps(self.index),encoding='utf-8')
        def download(args,**kwargs):
            target=Path(args[args.index('--dir')+1])
            for path in self.source.iterdir(): shutil.copyfile(path,target/path.name)
            return subprocess.CompletedProcess(args,0)
        def verifier(path,repo,ref,revision):
            self.calls.append((Path(path).name,repo,ref,revision))
            if Path(path).name==denied: raise ValueError('Synthetic signature refusal')
        def verify(*args): return verify_release.verify(*args,verifier=verifier)
        with patch.object(inputs.subprocess,'run',side_effect=download),patch.object(inputs,'verify',side_effect=verify),patch.object(inputs,'attest',side_effect=verifier):
            return inputs.prepare(self.expected,self.root/'candidate','windows')

    # Full candidate preparation authenticates index/all archives, then original native bytes.
    def test_original_raw_inputs_are_verified_after_complete_index(self):
        candidate=self.run_prepare()
        self.assertEqual(candidate['manifest'],self.index['builds']['windows'])
        self.assertEqual(len(self.calls),11)
        self.assertEqual(self.calls[0][0],'release-index.json')
        self.assertEqual([x[0] for x in self.calls[-2:]],['manifest.json','xallor-remote.exe'])
        self.assertTrue(all(x[1:]==('xallor-ai/xallor-remote','refs/tags/v0.3.0','a'*40) for x in self.calls))

    # Refusing a signed index prevents extraction and cannot produce a native execution input.
    def test_signature_refusal_stops_before_raw_preparation(self):
        with self.assertRaises(ValueError): self.run_prepare('release-index.json')
        self.assertEqual(len(self.calls),1)
        self.assertFalse((self.root/'candidate/native').exists())

    # Even individually accepted files cannot compensate for an incomplete signed inventory.
    def test_missing_index_member_is_not_filled_by_unindexed_download(self):
        self.index['files'].pop()
        with self.assertRaisesRegex(ValueError,'Incomplete'): self.run_prepare()
        self.assertFalse((self.root/'candidate/native').exists())

    # Indexed metadata and actual archived manifests must agree before extraction.
    def test_index_platform_identity_cannot_disagree_with_requested_version(self):
        self.index['builds']['linux']['build']['revision']='c'*40
        with self.assertRaises(ValueError): self.run_prepare()
        self.assertFalse((self.root/'candidate/native').exists())


if __name__=='__main__': unittest.main()
