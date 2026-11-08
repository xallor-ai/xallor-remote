"""Produce an artifact index and CycloneDX inventories from the shipped bytes."""
import argparse
import hashlib
import json
from pathlib import Path
import shutil
import subprocess
import sys
import tarfile
import zipfile

ROOT = Path(__file__).resolve().parents[1]


def sha(data):
    return hashlib.sha256(data).hexdigest()


def check_runtime(binary, manifest, platform):
    if (manifest['schema'] != 1 or manifest['build']['os'] != platform
            or manifest['build']['arch'] != 'amd64'
            or manifest['artifact']['sha256'] != sha(binary)
            or manifest['artifact']['size'] != len(binary)):
        raise ValueError('Runtime manifest differs from shipped binary')


def inspect_artifacts(directory):
    """Validate zip/tgz payloads without executing or extracting them."""
    directory = Path(directory)
    manifests = {}
    archives = []
    for platform in ('windows', 'linux'):
        matches = list(directory.glob(f'xallor-remote-*-{platform}-amd64.zip'))
        if len(matches) != 1:
            raise ValueError('Expected one zip for ' + platform)
        archive = matches[0]
        with zipfile.ZipFile(archive) as z:
            name = 'xallor-remote.exe' if platform == 'windows' else 'xallor-remote'
            if sorted(z.namelist()) != sorted([name, 'manifest.json']):
                raise ValueError('Unexpected zip members')
            manifest = json.loads(z.read('manifest.json'))
            if manifest['artifact']['filename'] != name:
                raise ValueError('Unexpected Runtime filename')
            check_runtime(z.read(name), manifest, platform)
        expected = sha(archive.read_bytes()) + '  ' + archive.name
        if Path(str(archive) + '.sha256').read_text(encoding='utf-8').strip() != expected:
            raise ValueError('Zip checksum differs')
        manifests[platform] = manifest
        archives.append(archive)
    tarballs = list(directory.glob('xallor-remote-mcp-*.tgz'))
    if len(tarballs) != 1:
        raise ValueError('Expected one MCP tarball')
    with tarfile.open(tarballs[0]) as tar:
        def read(name):
            members = [m for m in tar.getmembers() if m.name == 'package/' + name]
            if len(members) != 1 or not members[0].isfile():
                raise ValueError('Missing or duplicate regular member: ' + name)
            return tar.extractfile(members[0]).read()
        package = json.loads(read('package.json'))
        build = json.loads(read('build.json'))
        for platform, vendor in (('windows','win32-x64'), ('linux','linux-x64')):
            manifest = json.loads(read(f'vendor/{vendor}/manifest.json'))
            if manifest != manifests[platform]:
                raise ValueError('Vendor manifest differs from zip')
            check_runtime(read(f'vendor/{vendor}/' + manifest['artifact']['filename']), manifest, platform)
            for field in ('version','revision','dirty','protocol','profile_format'):
                if build[field] != manifest['build'][field]:
                    raise ValueError('MCP build identity differs: ' + field)
            if package['version'] != build['version']:
                raise ValueError('MCP version differs')
    archives += tarballs
    return archives, manifests


def generate(directory):
    archives, manifests = inspect_artifacts(directory)
    directory = Path(directory)
    evidence = []
    for platform, manifest in manifests.items():
        binary = directory / f'{platform}-amd64' / manifest['artifact']['filename']
        if sha(binary.read_bytes()) != manifest['artifact']['sha256']:
            raise ValueError('Expanded binary differs from zip')
        info = json.loads(subprocess.check_output(['go','version','-m','-json',str(binary)], encoding='utf-8'))
        components = [{'type':'library','name':m['Path'],'version':m['Version']}
                      for m in info.get('Deps', [])]
        components.append({'type':'application','name':'Go toolchain','version':info['GoVersion']})
        bom = {'bomFormat':'CycloneDX','specVersion':'1.5','version':1,
               'metadata':{'component':{'type':'application','name':'xallor-remote',
                                       'version':manifest['build']['version']}},'components':components}
        output = directory / f'runtime-{platform}-amd64.cdx.json'
        output.write_text(json.dumps(bom,indent=2)+'\n', encoding='utf-8')
        evidence.append(output)
    # npm's production dependency inventory supplements both vendor Runtime BOMs.
    npm = shutil.which('npm.cmd' if sys.platform == 'win32' else 'npm')
    bom = subprocess.check_output([npm,'sbom','--omit=dev','--sbom-format=cyclonedx'],
                                  cwd=ROOT/'packages/mcp', encoding='utf-8')
    json.loads(bom)
    output = directory/'mcp.cdx.json'
    output.write_text(bom, encoding='utf-8')
    evidence.append(output)
    files = archives + evidence + [Path(str(p)+'.sha256') for p in archives if p.suffix == '.zip']
    index = {'schema':1,'builds':manifests,'files':[
        {'name':p.name,'size':p.stat().st_size,'sha256':sha(p.read_bytes())} for p in files]}
    (directory/'release-index.json').write_text(json.dumps(index,indent=2)+'\n', encoding='utf-8')
    print('Validated zip/vendor manifests and generated SBOMs and release-index.json')


if __name__ == '__main__':
    sys.stdout.reconfigure(encoding='utf-8', errors='replace')
    parser = argparse.ArgumentParser()
    parser.add_argument('--directory', default=str(ROOT/'dist/release'))
    args = parser.parse_args()
    generate(args.directory)
