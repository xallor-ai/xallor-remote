package shell

import (
	"encoding/base64"
	"encoding/binary"
	"errors"
	"os/exec"
	"reflect"
	"strings"
	"testing"
	"unicode/utf16"
)

// Only auto on Linux may fall back from an absent bash to sh. Explicit shell,
// unsafe relative lookup, and another OS's syntax never silently switch.
func TestResolve_should_selectTargetShell_withoutRuntimeFallback(t *testing.T) {
	cases := []struct {
		os, name, want string
		present        map[string]bool
		fail           error
	}{
		{"windows", "auto", "powershell", map[string]bool{"powershell.exe": true}, nil},
		{"windows", "pwsh", "pwsh", map[string]bool{"pwsh.exe": true}, nil},
		{"windows", "bash", "", nil, ErrUnsupported},
		{"windows", "auto", "", map[string]bool{"pwsh.exe": true}, ErrUnavailable},
		{"linux", "auto", "sh", map[string]bool{"sh": true}, nil},
		{"linux", "bash", "", map[string]bool{"sh": true}, ErrUnavailable},
		{"linux", "pwsh", "pwsh", map[string]bool{"pwsh": true}, nil},
		{"linux", "/tmp/shell", "", nil, ErrUnsupported},
		{"linux", "bash -c", "", nil, ErrUnsupported},
		{"darwin", "auto", "", nil, ErrUnavailable},
	}
	for _, tc := range cases {
		t.Run(tc.os+"_"+tc.name+"_"+tc.want, func(t *testing.T) {
			a, _, err := resolve(tc.os, tc.name, func(file string) (string, error) {
				if tc.present[file] {
					return "/bin/" + file, nil
				}
				return "", exec.ErrNotFound
			})
			if !errors.Is(err, tc.fail) || a.name != tc.want {
				t.Fatal(a, err)
			}
		})
	}
	calls := 0
	_, _, err := resolve("linux", "auto", func(string) (string, error) { calls++; return "", exec.ErrDot })
	if !errors.Is(err, ErrUnavailable) || calls != 1 {
		t.Fatal("unsafe lookup fell back", calls, err)
	}
}

// The encoded PowerShell payload must preserve every character, including
// supplementary Unicode, quotes, newlines, backticks and dollar signs.
func TestArguments_should_preserveScript_withoutNativeArgumentReparsing(t *testing.T) {
	command := "$x = '中文 🌍 '' $HOME `';\nWrite-Output $x"
	for _, name := range []string{"powershell", "pwsh"} {
		args := arguments(name, command)
		if args[len(args)-2] != "-Command" {
			t.Fatal(args)
		}
		launcher := args[len(args)-1]
		payload := encode(command)
		if !strings.Contains(launcher, "FromBase64String('"+payload+"')") || strings.Contains(launcher, command) {
			t.Fatal("launcher did not preserve the encoded data boundary")
		}
		b, err := base64.StdEncoding.DecodeString(payload)
		if err != nil {
			t.Fatal(err)
		}
		words := make([]uint16, len(b)/2)
		for i := range words {
			words[i] = binary.LittleEndian.Uint16(b[2*i:])
		}
		if got := string(utf16.Decode(words)); got != command {
			t.Fatal(got)
		}
	}
	if !reflect.DeepEqual(arguments("bash", command), []string{"--noprofile", "--norc", "-c", command}) {
		t.Fatal("bash script changed")
	}
	if !reflect.DeepEqual(arguments("sh", command), []string{"-c", command}) {
		t.Fatal("sh script changed")
	}
}
