package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/xallor-ai/xallor-remote/internal/handoff"
)

// Both public handoff commands must accept the approved logical session ID;
// parsing the flags does not start or contact the user's Runtime.
func TestHandoffCommand_shouldAcceptConsentSession_forSubmitAndGet(t *testing.T) {
	for _, name := range []string{"submit", "get"} {
		t.Run(name, func(t *testing.T) {
			command, _, err := cmdHandoff().Find([]string{name})
			if err != nil {
				t.Fatal(err)
			}
			if err := command.ParseFlags([]string{"--session-id", "approved-consent"}); err != nil {
				t.Fatal("approved session cannot enter the public handoff command", err)
			}
			if got, err := command.Flags().GetString("session-id"); err != nil || got != "approved-consent" {
				t.Fatal(got, err)
			}
		})
	}
}

// Authorization belongs to the IPC envelope; the nested Cursor session and
// request ID remain unchanged. Omission must not invent consent or reuse an ID.
func TestHandoffCommand_shouldSendConsentSeparately_fromCursorSessionAndReceipt(t *testing.T) {
	for _, name := range []string{"submit", "get"} {
		for _, consent := range []string{"", "approved-consent"} {
			t.Run(name+"/"+consent, func(t *testing.T) {
				var method string
				var params map[string]any
				ensures, calls := 0, 0
				command := cmdHandoffWith(func() error { ensures++; return nil }, func(m string, p map[string]any) (map[string]any, error) {
					calls++
					method, params = m, p
					return map[string]any{"status": "completed", "exit_code": float64(0), "content": `{"receipt":{"request_id":"review-1","session_id":"cursor-session","state":"received"}}`}, nil
				})
				var output bytes.Buffer
				command.SetOut(&output)
				args := []string{name, "--device", "123456789"}
				var requestFile string
				const body = `{"request_id":"review-1","session_id":"cursor-session","agent":"cursor","mode":"ask","prompt":"review"}`
				if name == "submit" {
					requestFile = filepath.Join(t.TempDir(), "request.json")
					if err := os.WriteFile(requestFile, []byte(body), 0600); err != nil {
						t.Fatal(err)
					}
					args = append(args, "--request", requestFile)
				} else {
					args = append(args, "--request-id", "review-1")
				}
				if consent != "" {
					args = append(args, "--session-id", consent)
				}
				command.SetArgs(args)
				if err := command.Execute(); err != nil {
					t.Fatal(err)
				}
				if ensures != 1 || calls != 1 || method != "handoff."+name || params["device_id"] != "123456789" {
					t.Fatal("unexpected dispatch", ensures, calls, method)
				}
				if got, present := params["session_id"]; present != (consent != "") || (present && got != consent) {
					t.Fatal("authorization session changed or invented")
				}
				if name == "submit" {
					request, ok := params["request"].(handoff.Request)
					if !ok || request.Session != "cursor-session" || request.ID != "review-1" {
						t.Fatal("Cursor session or request identity overwritten")
					}
					data, err := os.ReadFile(requestFile)
					if err != nil || string(data) != body {
						t.Fatal("input file changed", err)
					}
				} else if params["request_id"] != "review-1" {
					t.Fatal("receipt identity changed")
				}
				var receipt handoff.Receipt
				if json.Unmarshal(output.Bytes(), &receipt) != nil || receipt.ID != "review-1" || receipt.Session != "cursor-session" {
					t.Fatal("receipt changed")
				}
			})
		}
	}
}

// A failed owner ensure must not issue a handoff or try any fallback transport.
func TestHandoffCommand_shouldNotCallRuntime_whenEnsureFails(t *testing.T) {
	want := errors.New("owner unavailable")
	calls := 0
	command := cmdHandoffWith(func() error { return want }, func(string, map[string]any) (map[string]any, error) { calls++; return nil, nil })
	command.SetArgs([]string{"get", "--device", "123456789", "--request-id", "review-1", "--session-id", "consent"})
	if err := command.Execute(); !errors.Is(err, want) || calls != 0 {
		t.Fatal("ensure failure dispatched", err, calls)
	}
}
