package execution

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/xallor-ai/xallor-remote/internal/shell"
)

var ErrProgramUnavailable = errors.New("target program unavailable")
var ErrUnsupportedProgram = errors.New("program requires explicit script invocation")

func Build(r Request, configuredShell, cwd string) (*exec.Cmd, error) {
	if err := r.Validate(); err != nil {
		return nil, err
	}
	if r.Script != nil {
		name := r.Script.Shell
		if name == "" {
			name = configuredShell
		}
		return shell.Build(name, r.Script.Text, cwd)
	}
	p := r.Program
	name := p.Executable
	// Explicit relative paths belong to the target working directory. A bare
	// executable uses the Runtime PATH; never implicitly search the workspace.
	if !filepath.IsAbs(name) {
		if filepath.VolumeName(name) != "" || runtime.GOOS == "windows" && strings.HasPrefix(strings.ReplaceAll(name, `/`, `\`), `\`) {
			return nil, ErrInvalid
		}
		if strings.ContainsRune(name, '/') || runtime.GOOS == "windows" && strings.ContainsRune(name, '\\') {
			name = filepath.Join(cwd, name)
		}
	}
	resolved, err := exec.LookPath(name)
	if err != nil {
		return nil, ErrProgramUnavailable
	}
	if runtime.GOOS == "windows" {
		base, ext := strings.ToLower(filepath.Base(resolved)), strings.ToLower(filepath.Ext(resolved))
		if ext == ".bat" || ext == ".cmd" || base == "cmd.exe" {
			return nil, ErrUnsupportedProgram
		}
	}
	if !nativeCommandFits(resolved, p.Args) {
		return nil, ErrTooLarge
	}
	cmd := exec.Command(resolved, p.Args...)
	cmd.Dir = cwd
	cmd.Env = os.Environ()
	return cmd, nil
}
