"""OS-neutral real CLI parsing regression; no Relay or Runtime is started."""
import argparse
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile

sys.stdout.reconfigure(encoding='utf-8',errors='replace')
ROOT = Path(__file__).resolve().parents[1]


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--binary',required=True)
    args = parser.parse_args()
    binary = Path(args.binary).resolve()
    results = []
    cases = [
        ('unknown_after_format',['exec','--jsonl','--unknown-option'],True),
        ('unknown_before_format',['exec','--unknown-option','--jsonl'],True),
        ('invalid_integer_before_format',['exec','--timeout-ms','not-an-int','--jsonl'],True),
        ('missing_value',['exec','--jsonl','--program'],True),
        ('invalid_format_value',['exec','--jsonl=invalid'],True),
        ('explicit_false',['exec','--jsonl=false','--unknown-option'],False),
        ('last_false_wins',['exec','--jsonl','--unknown-option','--jsonl=false'],False),
        ('last_true_wins',['exec','--jsonl=false','--unknown-option','--jsonl=true'],True),
        ('format_is_string_value',['exec','--unknown-option','--device','--jsonl'],False),
        ('format_is_equal_value',['exec','--unknown-option','--device=--jsonl'],False),
        ('format_after_separator',['exec','--unknown-option','--program','fixture','--','--jsonl'],False),
        ('false_after_separator_is_payload',['exec','--jsonl','--unknown-option','--','--jsonl=false'],True),
        ('semantic_error',['exec','--jsonl','--','echo','fragment'],True),
        ('text_error',['exec','--unknown-option'],False),
        ('another_command',['status','--unknown-option','--jsonl'],False),
        ('inherited_parse_error',['--require-profile-format','invalid','exec','--jsonl','--program','whoami'],True),
    ]
    with tempfile.TemporaryDirectory(prefix='xr-cli-parse-') as temporary:
        base = Path(temporary).resolve()
        for name,words,jsonl in cases:
            profile = base/name
            env = dict(os.environ,XALLOR_REMOTE_DATA_DIR=str(profile),
                       XALLOR_REMOTE_RELAY_URL='ws://127.0.0.1:1/remote')
            result = subprocess.run([str(binary),*words],env=env,capture_output=True,
                                    encoding='utf-8',errors='replace',timeout=15)
            assert result.returncode == 1, (name,result.returncode,result.stderr)
            assert result.stderr.strip(), (name,'missing human diagnostic')
            assert not profile.exists(), (name,'parsing initialized a Runtime profile')
            if jsonl:
                lines = result.stdout.splitlines()
                assert len(lines) == 1, (name,result.stdout,result.stderr)
                event = json.loads(lines[0])
                assert set(event) == {'type','code','message'}, (name,event)
                assert event['type'] == 'error' and event['code'] == 'invalid_execution' and event['message'], (name,event)
            else:
                assert result.stdout == '', (name,result.stdout)
            results.append({'name':name,'status':'pass','jsonl':jsonl})
        help_result = subprocess.run([str(binary),'exec','--jsonl','--help'],capture_output=True,
                                     encoding='utf-8',errors='replace',timeout=15)
        assert help_result.returncode == 0 and 'Usage:' in help_result.stdout
        results.append({'name':'help_remains_successful_text','status':'pass'})
    output = ROOT/'dist/verification/cli-argument-errors.json'
    output.parent.mkdir(parents=True,exist_ok=True)
    output.write_text(json.dumps({'platform':sys.platform,'results':results},indent=2),encoding='utf-8')
    print(json.dumps({'platform':sys.platform,'passed':len(results)}))


if __name__ == '__main__': main()
