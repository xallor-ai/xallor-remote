package execution

import (
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// Real child launches must resolve relative programs once, for either form of
// target working directory. Copy the test executable instead of relying on PATH.
func TestProgram_should_launchInsideWorkspace_withRelativeOrAbsolutePaths(t *testing.T) {
	original, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	t.Chdir(root)
	work := filepath.Join(root, "work")
	if err := os.Mkdir(work, 0700); err != nil {
		t.Fatal(err)
	}
	name := "tool"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	src, err := os.Open(original)
	if err != nil {
		t.Fatal(err)
	}
	defer src.Close()
	dst, err := os.OpenFile(filepath.Join(work, name), os.O_CREATE|os.O_WRONLY, 0700)
	if err != nil {
		t.Fatal(err)
	}
	_, copyErr := io.Copy(dst, src)
	closeErr := dst.Close()
	if copyErr != nil || closeErr != nil {
		t.Fatal(copyErr, closeErr)
	}
	t.Setenv("XR_RELATIVE_PROGRAM_HELPER", "1")
	for _, cwd := range []string{"work", work} {
		for _, executable := range []string{"./" + name, filepath.Join(work, name)} {
			cmd, err := Build(Request{Program: &Program{Executable: executable, Args: []string{"-test.run=^TestRelativeProgramHelper$"}}}, "", cwd)
			if err != nil {
				t.Fatal(err)
			}
			out, err := cmd.CombinedOutput()
			if err != nil || !strings.Contains(string(out), "relative-program-ok") {
				t.Fatalf("cwd=%q executable=%q launch=%v output=%s", cwd, executable, err, out)
			}
		}
	}
}

// The copied executable emits its marker only when explicitly selected as the child.
func TestRelativeProgramHelper(t *testing.T) {
	if os.Getenv("XR_RELATIVE_PROGRAM_HELPER") == "1" {
		cwd, err := os.Getwd()
		if err != nil || filepath.Base(cwd) != "work" {
			t.Fatal("incorrect child working directory", cwd, err)
		}
		_, _ = os.Stdout.WriteString("relative-program-ok\n")
	}
}
