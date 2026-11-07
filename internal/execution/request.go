// Package execution defines execution inputs independently of transports and
// constructs unstarted target-side commands. Job owners alone launch them.
package execution

import (
	"errors"
	"strings"
	"unicode/utf8"
)

const MaxInputBytes = 64 * 1024
const MaxJSONBytes = 6*MaxInputBytes + 8192
const MaxArguments = 256
const MaxTimeoutMS = 3_600_000

var ErrInvalid = errors.New("invalid execution input")
var ErrTooLarge = errors.New("execution input too large")

type Script struct {
	Text  string `json:"text"`
	Shell string `json:"shell,omitempty"`
}

type Program struct {
	Executable string   `json:"executable"`
	Args       []string `json:"args,omitempty"`
}

type Request struct {
	Script    *Script  `json:"script,omitempty"`
	Program   *Program `json:"program,omitempty"`
	Cwd       string   `json:"cwd,omitempty"`
	TimeoutMS int      `json:"timeout_ms,omitempty"`
}

// Decode rejects unknown fields, invalid source encoding and trailing values.
// IPC can decode its additional device_id envelope and call Validate separately.
func Decode(raw []byte) (Request, error) {
	var r Request
	if err := DecodeJSON(raw, &r); err != nil {
		return r, err
	}
	return r, r.Validate()
}

func validText(s string) bool { return utf8.ValidString(s) && !strings.ContainsRune(s, 0) }

func (r Request) Validate() error {
	if (r.Script == nil) == (r.Program == nil) || r.TimeoutMS < 0 || r.TimeoutMS > MaxTimeoutMS || !validText(r.Cwd) {
		return ErrInvalid
	}
	n := len(r.Cwd)
	if r.Script != nil {
		if r.Script.Text == "" || !validText(r.Script.Text) {
			return ErrInvalid
		}
		switch r.Script.Shell {
		case "", "auto", "powershell", "pwsh", "bash", "sh":
		default:
			return ErrInvalid
		}
		n += len(r.Script.Text) + len(r.Script.Shell)
	} else {
		p := r.Program
		if p.Executable == "" || !validText(p.Executable) {
			return ErrInvalid
		}
		if len(p.Args) > MaxArguments {
			return ErrTooLarge
		}
		n += len(p.Executable)
		for _, arg := range p.Args {
			if !validText(arg) {
				return ErrInvalid
			}
			n += len(arg)
		}
	}
	if n > MaxInputBytes {
		return ErrTooLarge
	}
	return nil
}
