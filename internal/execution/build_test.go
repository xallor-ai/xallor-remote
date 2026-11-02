package execution

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"
	"time"

	"github.com/xallor-ai/xallor-remote/internal/process"
)

// The native helper reports the arguments it actually received; no shell or
// parent-side reconstruction participates in the observed result.
func TestProgramArgumentsHelper(t *testing.T) {
	if os.Getenv("XR_EXECUTION_HELPER") != "1" {
		return
	}
	cwd, err := os.Getwd()
	if err != nil {
		os.Exit(43)
	}
	_ = json.NewEncoder(os.Stdout).Encode(struct {
		Args []string
		Cwd  string
	}{os.Args[3:], cwd})
	os.Exit(0)
}

// An owned native child must preserve empty, quoted, Unicode and metacharacter
// arguments, use the target cwd, and run independently of shell configuration.
func TestBuild_should_preserveNativeArguments_and_targetDirectory(t *testing.T) {
	t.Setenv("XR_EXECUTION_HELPER", "1")
	cwd := filepath.Join(t.TempDir(), "space ' 中文")
	if err := os.Mkdir(cwd, 0700); err != nil {
		t.Fatal(err)
	}
	args := []string{"", "a b", `a"b`, "a'b", "$HOME", "`", "a&b|c;d", "中文🌍", `C:\space path\`, "--flag=value"}
	r := Request{Program: &Program{Executable: os.Args[0], Args: append([]string{"-test.run=^TestProgramArgumentsHelper$", "--"}, args...)}}
	cmd, err := Build(r, "invalid-shell-does-not-apply", cwd)
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	child, err := process.Start(ctx, cmd)
	if err != nil {
		t.Fatal(err)
	}
	if err := child.Wait(); err != nil {
		t.Fatal(err, out.String())
	}
	var got struct {
		Args []string
		Cwd  string
	}
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatal(err, out.String())
	}
	if !reflect.DeepEqual(got.Args, args) || !samePath(got.Cwd, cwd) {
		t.Fatal(got)
	}
}

func samePath(a, b string) bool {
	x, err := os.Stat(a)
	if err != nil {
		return false
	}
	y, err := os.Stat(b)
	return err == nil && os.SameFile(x, y)
}

// Relative executable paths resolve against the supplied target directory,
// while unavailable programs and Windows batch interpreters cannot fall back.
func TestBuild_should_resolveRelativeProgram_and_rejectUnavailable(t *testing.T) {
	dir := t.TempDir()
	name := "fixture"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	b, err := os.ReadFile(os.Args[0])
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), b, 0700); err != nil {
		t.Fatal(err)
	}
	cmd, err := Build(Request{Program: &Program{Executable: "./" + name}}, "", dir)
	if err != nil || cmd.Path != filepath.Join(dir, name) {
		t.Fatal(cmd, err)
	}
	if _, err := Build(Request{Program: &Program{Executable: filepath.Join(dir, "missing")}}, "", dir); !errors.Is(err, ErrProgramUnavailable) {
		t.Fatal(err)
	}
	if runtime.GOOS == "windows" {
		if _, err := Build(Request{Program: &Program{Executable: "cmd.exe"}}, "", dir); !errors.Is(err, ErrUnsupportedProgram) {
			t.Fatal(err)
		}
		for _, ambiguous := range []string{"C:relative.exe", `\relative.exe`, "/relative.exe"} {
			if _, err := Build(Request{Program: &Program{Executable: ambiguous}}, "", dir); !errors.Is(err, ErrInvalid) {
				t.Fatal(ambiguous, err)
			}
		}
	}
}

// On Unix a backslash is a filename character, not a path separator. Bare
// executable names must still resolve through PATH instead of the target cwd.
func TestBuild_should_lookupBackslashFilenameOnPath_when_unix(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix filename semantics")
	}
	bin := t.TempDir()
	name := `tool\with-backslash`
	if err := os.WriteFile(filepath.Join(bin, name), []byte("#!/bin/sh\nexit 0\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	cmd, err := Build(Request{Program: &Program{Executable: name}}, "", t.TempDir())
	if err != nil || cmd.Path != filepath.Join(bin, name) {
		t.Fatal(cmd, err)
	}
}
