"""Installed files must remain the exact authenticated npm payload."""
import hashlib
import io
import json
from pathlib import Path
import tarfile
import tempfile
import unittest

from fixtures.package_payload import inspect_package, verify_installed_package


class PackagePayloadTests(unittest.TestCase):
    def setUp(self):
        temp=tempfile.TemporaryDirectory();self.addCleanup(temp.cleanup)
        self.root=Path(temp.name);self.archive=self.root/'release.tgz'
        build=dict(version='0.3.0',revision='a'*40,dirty=False,protocol=1,profile_format=1,os='windows',arch='amd64')
        binary=b'fixture, never executable'
        self.manifest=dict(schema=1,build=build,artifact=dict(filename='xallor-remote.exe',size=len(binary),sha256=hashlib.sha256(binary).hexdigest()))
        self.files={'package.json':json.dumps(dict(name='xallor-remote-mcp',version='0.3.0',bin={'xallor-remote':'dist/runtime-bin.js','xallor-remote-mcp':'dist/index.js'})).encode(),
                    'build.json':json.dumps(build).encode(),'dist/index.js':b'MCP fixture','dist/runtime-bin.js':b'CLI fixture',
                    'vendor/win32-x64/manifest.json':json.dumps(self.manifest).encode(),
                    'vendor/win32-x64/xallor-remote.exe':binary}

    def pack(self,extra=None):
        with tarfile.open(self.archive,'w:gz') as tar:
            for name,content in self.files.items():
                item=tarfile.TarInfo('package/'+name);item.size=len(content);tar.addfile(item,io.BytesIO(content))
            if extra:
                item=tarfile.TarInfo(extra[0]);item.type=extra[1];item.linkname='../../outside';tar.addfile(item)
        return inspect_package(self.archive,self.manifest)

    def install(self):
        package=self.root/'installed'
        for name,data in self.files.items():
            path=package/name;path.parent.mkdir(parents=True,exist_ok=True);path.write_bytes(data)
        return package

    # Dependencies are separate; every application payload file and entrypoint is bound.
    def test_accepts_exact_installed_payload_and_reports_entrypoints(self):
        inventory=self.pack();package=self.install()
        entry=verify_installed_package(package,inventory)
        self.assertEqual(entry['cli'],package/'dist/runtime-bin.js')
        self.assertEqual(entry['mcp'],package/'dist/index.js')

    # Altered, missing and injected entrypoint-adjacent files cannot borrow the tgz signature.
    def test_rejects_changed_missing_and_extra_installed_files(self):
        inventory=self.pack();package=self.install();entry=package/'dist/index.js'
        entry.write_bytes(b'changed')
        with self.assertRaises(ValueError):verify_installed_package(package,inventory)
        entry.unlink()
        with self.assertRaises(ValueError):verify_installed_package(package,inventory)
        entry.write_bytes(self.files['dist/index.js']);(package/'dist/injected.js').write_bytes(b'extra')
        with self.assertRaises(ValueError):verify_installed_package(package,inventory)

    # Tar namespace rejection happens before npm is allowed to process the archive.
    def test_rejects_traversal_links_and_duplicate_members(self):
        for name,kind in [('package/../outside',tarfile.REGTYPE),('package/dist/linked',tarfile.SYMTYPE),
                          ('package/dist/index.js',tarfile.REGTYPE),('package/C:/outside',tarfile.REGTYPE)]:
            with self.subTest(name=name),self.assertRaises(ValueError):self.pack((name,kind))

    # A valid-looking manifest cannot make a different release or wrapper path acceptable.
    def test_rejects_wrong_version_or_unexpected_bin_mapping(self):
        for change in ({'version':'0.2.6'},{'bin':{'xallor-remote':'../outside','xallor-remote-mcp':'dist/index.js'}}):
            metadata=json.loads(self.files['package.json']);metadata.update(change)
            self.files['package.json']=json.dumps(metadata).encode()
            with self.assertRaises(ValueError):self.pack()

    # Portable npm releases must not let Windows path aliases merge distinct subjects.
    def test_rejects_windows_device_and_trailing_alias_names(self):
        for name in ('package/dist/index.js.','package/dist/index.js ','package/CON.txt','package/NUL','package/COM1.log','package/dist/a|b'):
            with self.subTest(name=name),self.assertRaises(ValueError):self.pack((name,tarfile.REGTYPE))


if __name__=='__main__':unittest.main()
