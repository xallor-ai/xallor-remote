"""Strict npm application payload inspection; dependency provenance is separate."""
import hashlib
import json
import os
from pathlib import Path, PurePosixPath
import tarfile

ENTRYPOINTS = {'xallor-remote':'dist/runtime-bin.js', 'xallor-remote-mcp':'dist/index.js'}
MAX_FILE = 128*1024*1024
MAX_TOTAL = 512*1024*1024
RESERVED = {'CON','PRN','AUX','NUL','CLOCK$','CONIN$','CONOUT$'} | {prefix+digit for prefix in ('COM','LPT') for digit in '123456789¹²³'}


def digest_stream(stream, size):
    digest=hashlib.sha256();count=0
    while data := stream.read(min(1024*1024,size-count+1)):
        count += len(data)
        if count > size: raise ValueError('Payload grew beyond declared size')
        digest.update(data)
    if count != size: raise ValueError('Payload size differs')
    return digest.hexdigest()


def inspect_package(archive, manifest):
    platform='win32-x64' if manifest['build']['os']=='windows' else 'linux-x64'
    native='vendor/'+platform+'/'
    metadata_names={'package.json','build.json',native+'manifest.json'}
    files={};metadata={};seen=set();total=0
    with tarfile.open(archive,'r:gz') as tar:
        for count,member in enumerate(tar,1):
            if count>5000: raise ValueError('Too many npm payload members')
            name=member.name.rstrip('/') if member.isdir() else member.name
            path=PurePosixPath(name)
            if (name != path.as_posix() or not path.parts or path.parts[0]!='package'
                    or any(part in ('.','..','node_modules') for part in path.parts)
                    or any(char in name for char in '<>:"\\|?*') or any(ord(char)<32 for char in name)
                    or any(part!=part.rstrip(' .') or part.split('.')[0].upper() in RESERVED for part in path.parts)
                    or name.casefold() in seen):
                raise ValueError('Unsafe or duplicate npm payload namespace')
            seen.add(name.casefold())
            if member.isdir(): continue
            if not member.isfile() or len(path.parts)<2 or not 0<=member.size<=MAX_FILE:
                raise ValueError('Npm payload must contain bounded regular files')
            total+=member.size
            if total>MAX_TOTAL: raise ValueError('Npm payload exceeds inspection budget')
            relative='/'.join(path.parts[1:])
            with tar.extractfile(member) as stream:
                if relative in metadata_names:
                    if member.size>65536: raise ValueError('Oversized npm metadata')
                    raw=stream.read();metadata[relative]=raw
                    if len(raw)!=member.size: raise ValueError('Incomplete npm metadata')
                    digest=hashlib.sha256(raw).hexdigest()
                else:
                    digest=digest_stream(stream,member.size)
            files[relative]={'size':member.size,'sha256':digest}
    file_names={name.casefold() for name in files}
    if any(parent.as_posix().casefold() in file_names for name in files for parent in PurePosixPath(name).parents):
        raise ValueError('Npm payload file collides with a directory')
    if not metadata_names.issubset(metadata) or not set(ENTRYPOINTS.values()).issubset(files):
        raise ValueError('Npm package lacks required metadata or entrypoints')
    package=json.loads(metadata['package.json']);build=json.loads(metadata['build.json'])
    if (package.get('name')!='xallor-remote-mcp' or package.get('bin')!=ENTRYPOINTS
            or package.get('version')!=manifest['build']['version']
            or json.loads(metadata[native+'manifest.json'])!=manifest):
        raise ValueError('Npm package identity/entrypoints differ from candidate')
    for key in ('version','revision','dirty','protocol','profile_format'):
        if build.get(key)!=manifest['build'][key] or type(build.get(key)) is not type(manifest['build'][key]):
            raise ValueError('Package build differs from native candidate')
    binary=files.get(native+manifest['artifact']['filename'])
    if binary!={key:manifest['artifact'][key] for key in ('size','sha256')}:
        raise ValueError('Packaged native bytes differ from authenticated bundle')
    return {'files':files,'package':package,'build':build,'manifest':manifest,
            'manifest_sha256':hashlib.sha256(metadata[native+'manifest.json']).hexdigest()}


def redirected(path):
    return path.is_symlink() or getattr(path,'is_junction',lambda:False)()


def verify_installed_package(package, inventory):
    package=Path(package).absolute()
    # Windows 8.3 spellings can resolve to a different text path without a
    # redirect. Inspect actual components rather than equating spelling/identity.
    if any(redirected(path) for path in (package,*package.parents)) or not package.is_dir():
        raise ValueError('Installed application directory is redirected or absent')
    actual=set()
    for directory,dirs,filenames in os.walk(package,followlinks=False):
        parent=Path(directory)
        for name in dirs+filenames:
            if redirected(parent/name): raise ValueError('Installed application contains a redirect')
        # npm dependencies are not attested application payload. Record the
        # generated install lock separately; never claim it inherits the tgz signature.
        if parent==package and 'node_modules' in dirs: dirs.remove('node_modules')
        for name in filenames:
            path=parent/name;relative=path.relative_to(package).as_posix();actual.add(relative)
            expected=inventory['files'].get(relative)
            if expected is None or not path.is_file() or path.stat().st_size!=expected['size']:
                raise ValueError('Installed payload file set/size differs: '+relative)
            with path.open('rb') as stream:
                if digest_stream(stream,expected['size'])!=expected['sha256']:
                    raise ValueError('Installed payload bytes differ: '+relative)
    if actual!=set(inventory['files']): raise ValueError('Installed payload is incomplete')
    return {'cli':package/ENTRYPOINTS['xallor-remote'],'mcp':package/ENTRYPOINTS['xallor-remote-mcp']}
