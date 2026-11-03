// Package shell selects target-side interpreters and constructs unstarted
// commands. It never executes, translates syntax, retries, or owns processes.
package shell

import (
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

var ErrUnsupported = errors.New("unsupported shell")
var ErrUnavailable = errors.New("shell unavailable")
var ErrTooLarge = errors.New("shell command too large")

type adapter struct{ name, executable string }

func adapters(goos string) []adapter {
	switch goos {
	case "windows":
		return []adapter{{"powershell", "powershell.exe"}, {"pwsh", "pwsh.exe"}}
	case "linux":
		return []adapter{{"bash", "bash"}, {"sh", "sh"}, {"pwsh", "pwsh"}}
	default:
		return nil
	}
}

func Validate(name string) error { return validate(runtime.GOOS, name) }
func validate(goos, name string) error {
	if name == "" || name == "auto" {
		return nil
	}
	for _, a := range adapters(goos) {
		if name == a.name {
			return nil
		}
	}
	return ErrUnsupported
}

// Empty is the storage representation of an unconfigured default, not an
// alias for another interpreter. Explicit choices never fall back.
func resolve(goos, name string, lookup func(string) (string, error)) (adapter, string, error) {
	if err := validate(goos, name); err != nil {
		return adapter{}, "", err
	}
	candidates := adapters(goos)
	if name == "" || name == "auto" {
		if goos == "windows" {
			candidates = candidates[:1]
		}
		if goos == "linux" {
			candidates = candidates[:2]
		}
	}
	for _, a := range candidates {
		if name != "" && name != "auto" && a.name != name {
			continue
		}
		path, err := lookup(a.executable)
		if err == nil {
			return a, path, nil
		}
		// Do not reinterpret unsafe relative-path or permission errors as absence.
		if !errors.Is(err, exec.ErrNotFound) {
			return adapter{}, "", ErrUnavailable
		}
	}
	return adapter{}, "", ErrUnavailable
}

type Capability struct {
	Configured string   `json:"configured"`
	Selected   string   `json:"selected,omitempty"`
	Available  []string `json:"available"`
	Code       string   `json:"code,omitempty"`
}

// Describe probes executable availability only: it runs no user profile or
// version command, and does not imply a successful execution or login.
func Describe(name string) Capability {
	if name == "" {
		name = "auto"
	}
	c := Capability{Configured: name, Available: []string{}}
	for _, a := range adapters(runtime.GOOS) {
		if _, err := exec.LookPath(a.executable); err == nil {
			c.Available = append(c.Available, a.name)
		}
	}
	a, _, err := resolve(runtime.GOOS, name, exec.LookPath)
	if err != nil {
		c.Code = "shell_unavailable"
		if errors.Is(err, ErrUnsupported) {
			c.Code = "unsupported_shell"
		}
	} else {
		c.Selected = a.name
	}
	return c
}

const encodingPrefix = "[Console]::OutputEncoding = [Text.UTF8Encoding]::new($false); $OutputEncoding = [Console]::OutputEncoding; "

// Capture the last statement's success inside the script block. WinPS resets
// $? after invoking a script block containing a non-terminating error. Explicit
// exit still leaves immediately, and stale native LASTEXITCODE is never used.
const statusSuffix = "\nif (-not $?) { exit 1 } else { exit 0 }"

func encode(command string) string {
	// Encoding setup belongs to the launcher: using/param declarations must
	// retain their required leading position in the user's script block.
	words := utf16.Encode([]rune(command + statusSuffix))
	b := make([]byte, 2*len(words))
	for i, w := range words {
		binary.LittleEndian.PutUint16(b[2*i:], w)
	}
	return base64.StdEncoding.EncodeToString(b)
}

func arguments(name, command string) []string {
	switch name {
	case "powershell", "pwsh":
		// -EncodedCommand forces redirected WinPS stderr to CLIXML even with
		// -OutputFormat Text. A fixed ASCII launcher decodes the script instead;
		// only base64 data is inserted, never user text or path quoting.
		launcher := encodingPrefix + "& ([scriptblock]::Create([Text.Encoding]::Unicode.GetString([Convert]::FromBase64String('" + encode(command) + "'))))"
		return []string{"-NoLogo", "-NoProfile", "-NonInteractive", "-Command", launcher}
	case "bash":
		return []string{"--noprofile", "--norc", "-c", command}
	default:
		return []string{"-c", command}
	}
}

// Build leaves startup/cancellation/cleanup to the job owner. No controller OS
// assumptions or shell quoting are applied to the target's command text.
func Build(name, command, cwd string) (*exec.Cmd, error) {
	if !utf8.ValidString(command) || strings.IndexByte(command, 0) >= 0 || command == "" {
		return nil, fmt.Errorf("invalid command text")
	}
	a, path, err := resolve(runtime.GOOS, name, exec.LookPath)
	if err != nil {
		return nil, err
	}
	args := arguments(a.name, command)
	// Windows CreateProcess is limited to 32767 UTF-16 code units including NUL.
	// Reserve quoting overhead for every argument. POSIX exec has platform limits
	// too; its own start error remains authoritative and never triggers a retry.
	if runtime.GOOS == "windows" {
		units := len(utf16.Encode([]rune(path))) + 3
		for _, arg := range args {
			units += len(utf16.Encode([]rune(arg))) + 3
		}
		if units >= 32767 {
			return nil, ErrTooLarge
		}
	}
	cmd := exec.Command(path, args...)
	cmd.Dir = cwd
	cmd.Env = os.Environ()
	return cmd, nil
}
