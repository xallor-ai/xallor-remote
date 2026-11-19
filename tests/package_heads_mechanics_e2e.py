"""Native installed-head observation mechanics, explicitly not npm/signature acceptance."""
import argparse
import json
from pathlib import Path
import shutil
import sys

from fixtures.installed_package_heads import run_installed_heads
from fixtures.owner_mechanics import build_owner_mechanics
from fixtures.signed_cycle_report import SignedCycleReport

ROOT=Path(__file__).resolve().parents[1]


def main():
    p=argparse.ArgumentParser();p.add_argument('--observer',required=True);p.add_argument('--go',default='go')
    p.add_argument('--fixture-binary');p.add_argument('--node',default=shutil.which('node'))
    p.add_argument('--modules',default=str(ROOT/'packages/mcp/node_modules'))
    p.add_argument('--output',required=True);args=p.parse_args()
    output=Path(args.output).resolve()
    protected={Path(args.observer).resolve(),Path(sys.executable).resolve()}
    if args.fixture_binary:protected.add(Path(args.fixture_binary).resolve())
    if args.node:protected.add(Path(args.node).resolve())
    sources=[Path(args.modules).resolve(),ROOT/'packages/mcp']
    if output.suffix!='.json' or output in protected or any(output==p or p in output.parents for p in sources):
        p.error('Report must not overwrite executable or package inputs')
    cycle=SignedCycleReport(output);error=None
    cycle.data.update(scope='copied current package heads and native controlled verifier; no npm install or cryptographic acceptance',cryptographic_acceptance=False)
    try:
        work=cycle.create_workspace()
        binary,fixture_env,evidence=build_owner_mechanics(work,args.go,args.fixture_binary)
        cycle.data['fixture']=evidence
        modules=work/'prefix/node_modules';shutil.copytree(args.modules,modules)
        package=modules/'xallor-remote-mcp';package.mkdir()
        shutil.copytree(ROOT/'packages/mcp/dist',package/'dist')
        shutil.copyfile(ROOT/'packages/mcp/package.json',package/'package.json')
        raw=(binary.parent/'manifest.json').read_bytes();manifest=json.loads(raw)
        (package/'build.json').write_text(json.dumps(manifest['build']),encoding='utf-8')
        vendor=package/'vendor'/('win32-x64' if sys.platform=='win32' else 'linux-x64');vendor.mkdir(parents=True)
        shutil.copyfile(binary,vendor/binary.name);(vendor/binary.name).chmod(0o700)
        (vendor/'manifest.json').write_bytes(raw)
        import os
        env={key:value for key,value in os.environ.items() if not key.upper().startswith(('XALLOR_','XR_TEST_'))}
        env.update(fixture_env)
        run_installed_heads(cycle,dict(cli=package/'dist/runtime-bin.js',mcp=package/'dist/index.js'),
                            str(Path(args.node).resolve()),Path(args.observer).resolve(),manifest,raw,env)
        cycle.data['passed']=True
    except Exception as caught:error=caught
    finally:
        if cycle.data.get('heads_cleanup_confirmed'):
            try:cycle.remove_workspace()
            except Exception as caught:error=error or caught
        cycle.finish(error)
    print(json.dumps(dict(passed=cycle.data['passed'],output=str(cycle.output),failure=cycle.data.get('failure'),cleanup_errors=cycle.data['cleanup_errors'])))
    return 0 if cycle.data['passed'] else 1


if __name__=='__main__':
    sys.stdout.reconfigure(encoding='utf-8',errors='replace');sys.exit(main())
