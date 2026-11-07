package shell

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/xallor-ai/xallor-remote/internal/process"
)

func powerShellStreams(t *testing.T, name, script string) (string, string, int) {
	t.Helper()
	cmd, err := Build(name, script, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	child, err := process.Start(ctx, cmd)
	if err != nil {
		t.Fatal(err)
	}
	err = child.Wait()
	code := 0
	if err != nil {
		var exit *exec.ExitError
		if errors.Is(err, process.ErrCleanup) || !errors.As(err, &exit) {
			t.Fatal(err)
		}
		code = exit.ExitCode()
	}
	return strings.ReplaceAll(stdout.String(), "\r\n", "\n"), stderr.String(), code
}

// Real interpreters must keep named-block grammar and last-statement failure
// semantics. Separate streams distinguish execution from a parser echoing code.
func TestBuild_should_preservePowerShellBlocks_andTerminalStatus(t *testing.T) {
	names := []string{"pwsh"}
	if runtime.GOOS == "windows" {
		names = append(names, "powershell")
	}
	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			if _, err := exec.LookPath(name); err != nil {
				if name == "powershell" {
					t.Fatal(err)
				}
				t.Skip("optional pwsh is not installed")
			}
			type blockCase struct {
				name, script, stdout, stderr string
				code                         int
			}
			cases := []blockCase{
				{"all_blocks", "begin {'begin'} process {'process'} end {'end'}", "begin\nprocess\nend\n", "", 0},
				{"begin_only", "begin {'begin'}", "begin\n", "", 0},
				{"process_only", "process {'process'}", "process\n", "", 0},
				{"declarations", "using namespace System.Text\nparam($Label='中文🌍')\nbegin {'begin'} end {[StringBuilder]::new($Label).ToString()}", "begin\n中文🌍\n", "", 0},
				{"end_error", "begin {'before-error'} end {Write-Error 'named-failure'}", "before-error\n", "named-failure", 1},
				{"process_error", "process {'before-error'; Write-Error 'named-failure'}", "before-error\n", "named-failure", 1},
				{"explicit_exit", "end {'before-exit'; exit 7}", "before-exit\n", "", 7},
				{"literal_block_text", "$payload=@'\nbegin { text }\n'@\n'before-error'; Write-Error 'named-failure'", "before-error\n", "named-failure", 1},
				{"nested_block", "function Inner {\n begin {'unused'}\n}\n'before-error'; Write-Error 'named-failure'", "before-error\n", "named-failure", 1},
				{"trailing_comment", "end {'done' # comment\n}", "done\n", "", 0},
				{"dynamicparam_only", "dynamicparam { [System.Management.Automation.RuntimeDefinedParameterDictionary]::new() }", "", "", 0},
				{"dynamic_binding_required", "[CmdletBinding()]\nparam()\ndynamicparam { $d=[System.Management.Automation.RuntimeDefinedParameterDictionary]::new(); $attrs=[System.Collections.ObjectModel.Collection[System.Attribute]]::new(); $a=[System.Management.Automation.ParameterAttribute]::new(); $a.Mandatory=$true; $attrs.Add($a); $d.Add('Required',[System.Management.Automation.RuntimeDefinedParameter]::new('Required',[string],$attrs)); $d }", "", "Required", 1},
			}
			out, _, code := powerShellStreams(t, name, "[bool][System.Management.Automation.Language.ScriptBlockAst].GetProperty('CleanBlock')")
			if code == 0 && strings.TrimSpace(out) == "True" {
				cases = append(cases,
					blockCase{"clean", "end {'done'} clean {[Console]::Error.WriteLine('cleaned')}", "done\n", "cleaned", 0},
					blockCase{"clean_only", "clean {[Console]::Error.WriteLine('cleaned')}", "", "cleaned", 0},
					blockCase{"clean_explicit_exit", "end {exit 7} clean {[Console]::Error.WriteLine('cleaned')}", "", "cleaned", 7})
			}
			for _, tc := range cases {
				t.Run(tc.name, func(t *testing.T) {
					out, stderr, code := powerShellStreams(t, name, tc.script)
					if code != tc.code || out != tc.stdout || (tc.stderr == "" && stderr != "") || !strings.Contains(stderr, tc.stderr) {
						t.Fatalf("exit=%d stdout=%q stderr=%q", code, out, stderr)
					}
				})
			}
		})
	}
}
