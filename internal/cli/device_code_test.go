package cli

import (
	"bytes"
	"errors"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// Parse the actual command tree without starting a Runtime. Secret material
// must no longer be accepted as a process argument; stdin is an explicit mode.
func TestDeviceCodeParser_shouldAcceptStdin_andRejectSecretArgument(t *testing.T) {
	for _, tc := range []struct {
		name   string
		args   []string
		wantOK bool
	}{
		{"stdin", []string{"--code-id", "old", "--new-code-id", "new", "--secret-stdin"}, true},
		{"plaintext-argument", []string{"--code-id", "old", "--new-code-id", "new", "--new-secret", "argument-canary-private"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cmd, _, err := cmdDevice().Find([]string{"code", "rotate"})
			if err != nil {
				t.Fatal(err)
			}
			err = cmd.ParseFlags(tc.args)
			if (err == nil) != tc.wantOK {
				t.Fatalf("secret input mode accepted=%v want=%v", err == nil, tc.wantOK)
			}
		})
	}
}

// Real Cobra parsing and bounded stdin/env consumption precede the runner
// boundary. Invalid or ambiguous input must not start/contact Runtime.
func TestDeviceCodeSecret_shouldValidateBeforeRunner_withoutEchoingInput(t *testing.T) {
	for _, tc := range []struct {
		name, env, input, want string
		configured, stdin, ok  bool
	}{
		{name: "env", configured: true, env: "private-env-canary", want: "private-env-canary", ok: true},
		{name: "missing"},
		{name: "empty-env", configured: true},
		{name: "empty-stdin", stdin: true},
		{name: "lf", stdin: true, input: "private-stdin-canary\n", want: "private-stdin-canary", ok: true},
		{name: "crlf", stdin: true, input: "private-stdin-canary\r\n", want: "private-stdin-canary", ok: true},
		{name: "spaces", stdin: true, input: "  private-canary  \n", want: "  private-canary  ", ok: true},
		{name: "lone-cr", stdin: true, input: "private-canary\r", want: "private-canary\r", ok: true},
		{name: "two-newlines", stdin: true, input: "private-canary\n\n", want: "private-canary\n", ok: true},
		{name: "limit", stdin: true, input: strings.Repeat("q", 4096), want: strings.Repeat("q", 4096), ok: true},
		{name: "limit-lf", stdin: true, input: strings.Repeat("q", 4096) + "\n", want: strings.Repeat("q", 4096), ok: true},
		{name: "limit-crlf", stdin: true, input: strings.Repeat("q", 4096) + "\r\n", want: strings.Repeat("q", 4096), ok: true},
		{name: "over-limit", stdin: true, input: strings.Repeat("q", 4097) + "\r\n"},
		{name: "env-over-limit", configured: true, env: strings.Repeat("q", 4097)},
		{name: "invalid-utf8", stdin: true, input: string([]byte{0xff})},
		{name: "nul", stdin: true, input: "private\x00canary"},
		{name: "dual", configured: true, env: "private-env-canary", stdin: true, input: "private-stdin-canary"},
		{name: "empty-env-and-stdin", configured: true, stdin: true, input: "private-stdin-canary"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			clearCodeSecretEnv(t)
			if tc.configured {
				t.Setenv("XALLOR_REMOTE_CODE_SECRET", tc.env)
			}
			var output bytes.Buffer
			input := &countCodeReader{Reader: strings.NewReader(tc.input)}
			calls := 0
			cmd := cmdDeviceCode(func(_ *cobra.Command, params map[string]any) error {
				calls++
				if !tc.ok {
					t.Error("invalid local input reached Runtime runner")
					return nil
				}
				if params["new_secret"] != tc.want || params["action"] != "code.rotate" || params["code_id"] != "original" || params["new_code_id"] != "replacement" || len(params) != 4 {
					t.Error("input bytes or management request changed")
				}
				return nil
			})
			cmd.SilenceErrors, cmd.SilenceUsage = true, true
			cmd.SetOut(&output)
			cmd.SetErr(&output)
			cmd.SetIn(input)
			args := []string{"rotate", "--code-id", "original", "--new-code-id", "replacement"}
			if tc.stdin {
				args = append(args, "--secret-stdin")
			}
			cmd.SetArgs(args)
			err := cmd.Execute()
			if (err == nil) != tc.ok || (calls == 1) != tc.ok {
				t.Fatalf("validation result mismatch: ok=%v calls=%d", err == nil, calls)
			}
			if tc.configured && tc.stdin && input.reads != 0 {
				t.Error("ambiguous source consumed stdin")
			}
			if output.Len() != 0 {
				t.Error("local input handling emitted data")
			}
			if err != nil && strings.Contains(err.Error(), "private") {
				t.Error("error echoed private value")
			}
		})
	}
}

type countCodeReader struct {
	io.Reader
	reads int
}

func (r *countCodeReader) Read(p []byte) (int, error) { r.reads++; return r.Reader.Read(p) }

type unlimitedCodeReader struct{ bytes int }

func (r *unlimitedCodeReader) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 'x'
	}
	r.bytes += len(p)
	return len(p), nil
}

type failedCodeReader struct{}

func (failedCodeReader) Read([]byte) (int, error) {
	return 0, errors.New("private-reader-error-canary")
}

// Oversized streams are not drained and underlying I/O errors or invalid flag
// values cannot leak the reader's contents through command diagnostics.
func TestDeviceCodeSecret_shouldBoundReads_andSanitizeLocalFailures(t *testing.T) {
	for _, kind := range []string{"unbounded", "reader-error", "old-argument", "boolean-value", "positional", "invalid-id"} {
		t.Run(kind, func(t *testing.T) {
			clearCodeSecretEnv(t)
			stream := &unlimitedCodeReader{}
			var input io.Reader = stream
			args := []string{"rotate", "--code-id", "original", "--new-code-id", "replacement", "--secret-stdin"}
			switch kind {
			case "reader-error":
				input = failedCodeReader{}
			case "old-argument":
				args = []string{"rotate", "--code-id", "original", "--new-code-id", "replacement", "--new-secret", "private-argument-canary"}
			case "boolean-value":
				args = []string{"rotate", "--code-id", "original", "--new-code-id", "replacement", "--secret-stdin=private-flag-canary"}
			case "positional":
				args = append(args, "private-positional-canary")
			case "invalid-id":
				args = []string{"rotate", "--code-id", " original ", "--new-code-id", "replacement"}
				t.Setenv("XALLOR_REMOTE_CODE_SECRET", "private-env-canary")
			}
			cmd := cmdDeviceCode(func(*cobra.Command, map[string]any) error {
				t.Error("invalid input reached Runtime runner")
				return nil
			})
			var output bytes.Buffer
			cmd.SilenceErrors, cmd.SilenceUsage = true, true
			cmd.SetOut(&output)
			cmd.SetErr(&output)
			cmd.SetIn(input)
			cmd.SetArgs(args)
			err := cmd.Execute()
			if err == nil {
				t.Fatal("invalid input accepted")
			}
			if strings.Contains(err.Error(), "private") || strings.Contains(output.String(), "private") {
				t.Fatal("local error exposed secret material")
			}
			if kind == "unbounded" && stream.bytes != 4099 {
				t.Fatalf("unbounded input consumption: %d", stream.bytes)
			}
			if kind != "unbounded" && kind != "reader-error" && stream.bytes != 0 {
				t.Fatal("invalid arguments consumed stdin")
			}
		})
	}
}

func clearCodeSecretEnv(t *testing.T) {
	t.Helper()
	t.Setenv("XALLOR_REMOTE_CODE_SECRET", "")
	if err := os.Unsetenv("XALLOR_REMOTE_CODE_SECRET"); err != nil {
		t.Fatal(err)
	}
}
