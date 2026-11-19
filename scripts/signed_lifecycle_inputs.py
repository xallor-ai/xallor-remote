"""Authenticate complete release candidates before exposing native executable inputs."""
import json
import hashlib
from pathlib import Path
import re
import stat
import subprocess
import zipfile

from release_evidence import check_runtime, inspect_artifacts
from verify_release import attest, verify

REPOSITORY = 'xallor-ai/xallor-remote'
TAG = r'v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(?:-[0-9A-Za-z]+(?:[.-][0-9A-Za-z]+)*)?'


def request(env):
    candidates = []
    for prefix in ('FIRST', 'SECOND'):
        tag, revision = env.get(prefix+'_TAG', ''), env.get(prefix+'_REVISION', '')
        if len(tag) > 100 or not re.fullmatch(TAG, tag) or not re.fullmatch('[0-9a-f]{40}', revision):
            raise ValueError('Expected release version tag and full lowercase commit: '+prefix)
        candidates.append(dict(tag=tag, version=tag[1:], revision=revision))
    first, second = candidates
    if first['tag'] == second['tag'] or first['revision'] == second['revision']:
        raise ValueError('Two distinct version tags and source commits are required')
    expected = dict(GITHUB_REPOSITORY=REPOSITORY, GITHUB_EVENT_NAME='workflow_dispatch',
                    GITHUB_REF_TYPE='tag', GITHUB_REF_PROTECTED='true',
                    GITHUB_REF='refs/tags/'+second['tag'], GITHUB_SHA=second['revision'])
    if any(env.get(key) != value for key, value in expected.items()):
        raise ValueError('Acceptance must run from the protected second tag and exact source commit')
    return candidates


def artifact_names(version):
    zips = [f'xallor-remote-{version}-{osname}-amd64.zip' for osname in ('windows', 'linux')]
    return set(zips + [name+'.sha256' for name in zips] + [f'xallor-remote-mcp-{version}.tgz',
               'runtime-windows-amd64.cdx.json', 'runtime-linux-amd64.cdx.json', 'mcp.cdx.json'])


def check_identity(manifest, expected, platform):
    build = manifest.get('build', {})
    values = dict(version=expected['version'], revision=expected['revision'], os=platform, arch='amd64')
    if build.get('dirty') is not False or any(build.get(key) != value for key, value in values.items()):
        raise ValueError('Manifest does not identify the requested clean platform release')


def raw_members(archive, platform):
    binary = 'xallor-remote.exe' if platform == 'windows' else 'xallor-remote'
    limits = {binary: 128*1024*1024, 'manifest.json': 65536}
    members = archive.infolist()
    if sorted(m.filename for m in members) != sorted(limits):
        raise ValueError('Expected exactly two Runtime archive members')
    for member in members:
        kind = stat.S_IFMT(member.external_attr >> 16)
        if (member.is_dir() or kind not in (0, stat.S_IFREG) or member.flag_bits & 1
                or not 0 < member.file_size <= limits[member.filename]):
            raise ValueError('Runtime archive member must be bounded, regular and unencrypted')
    return binary


def extract_raw(archive, destination, platform):
    # Validate the entire namespace and bytes before creating any output directory.
    with zipfile.ZipFile(archive) as z:
        name = raw_members(z, platform)
        raw = z.read('manifest.json')
        manifest = json.loads(raw)
        data = z.read(name)
    if manifest['artifact']['filename'] != name:
        raise ValueError('Unexpected manifest executable name')
    check_runtime(data, manifest, platform)
    destination.mkdir(parents=True, exist_ok=False)
    for filename, content in ((name, data), ('manifest.json', raw)):
        with (destination/filename).open('xb') as stream:
            stream.write(content)
    (destination/name).chmod(0o700)
    return manifest


def prepare(expected, destination, platform):
    destination = Path(destination)
    destination.mkdir(parents=True, exist_ok=False)
    names = artifact_names(expected['version'])
    args = ['gh', 'release', 'download', expected['tag'], '--repo', REPOSITORY, '--dir', str(destination)]
    for name in sorted(names | {'release-index.json'}):
        args += ['--pattern', name]
    result = subprocess.run(args, capture_output=True, timeout=600)
    if result.returncode:
        # Do not persist download redirect URLs or credential-bearing diagnostics.
        raise RuntimeError('Candidate download failed; gh exit '+str(result.returncode))
    source_ref = 'refs/tags/'+expected['tag']
    verify(destination, REPOSITORY, source_ref, expected['revision'])
    index = json.loads((destination/'release-index.json').read_bytes())
    if {entry['name'] for entry in index['files']} != names or set(index['builds']) != {'windows', 'linux'}:
        raise ValueError('Incomplete or unexpected candidate index')
    for osname in ('windows', 'linux'):
        check_identity(index['builds'][osname], expected, osname)
        with zipfile.ZipFile(destination/f"xallor-remote-{expected['version']}-{osname}-amd64.zip") as archive:
            raw_members(archive, osname)
    _, manifests = inspect_artifacts(destination)
    if manifests != index['builds']:
        raise ValueError('Signed index differs from archived manifests')
    bundle = destination/'native'
    manifest = extract_raw(destination/f"xallor-remote-{expected['version']}-{platform}-amd64.zip", bundle, platform)
    for path in (bundle/'manifest.json', bundle/manifest['artifact']['filename']):
        attest(path, REPOSITORY, source_ref, expected['revision'])
    return dict(expected, bundle=str(bundle.resolve()), manifest=manifest,
                manifest_sha256=hashlib.sha256((bundle/'manifest.json').read_bytes()).hexdigest(), indexed_artifacts=index['files'])
