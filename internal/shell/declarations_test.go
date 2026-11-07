package shell

import (
	"os/exec"
	"runtime"
	"strings"
	"testing"
)

// Leading declarations remain first in the user script, while the launcher
// still supplies Unicode output encoding and the existing exit-status policy.
func TestBuild_should_preserveLeadingPowerShellDeclarations(t *testing.T) {
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
			for _, tc := range []struct{ name, script, want string }{
				{"using", "using namespace System.Text\n[StringBuilder]::new('using-ready 中文').ToString()", "using-ready 中文"},
				{"param", "param([string]$Label = 'param-ready 中文')\nWrite-Output $Label", "param-ready 中文"},
				{"advanced_param", "using namespace System.Text\n[CmdletBinding()]\nparam([string]$Label = 'advanced-ready 🌍')\n[StringBuilder]::new($Label).ToString()", "advanced-ready 🌍"},
			} {
				t.Run(tc.name, func(t *testing.T) {
					out, code := run(t, name, tc.script)
					if code != 0 || strings.TrimSpace(out) != tc.want {
						t.Fatalf("code=%d output=%q", code, out)
					}
				})
			}
		})
	}
}
