package handoff

import (
	"encoding/json"
	"strings"
	"testing"
)

const testSession = "12345678-1234-1234-1234-123456789abc"

// A matching init and result are required; process EOF and assistant text cannot claim completion.
func TestCursor_should_requireMatchingSessionWorkspaceAndTerminal(t *testing.T) {
	workspace := t.TempDir()
	line := func(v any) string { b, _ := json.Marshal(v); return string(b) + "\n" }
	init := line(map[string]any{"type": "system", "subtype": "init", "session_id": testSession, "cwd": workspace})
	result := line(map[string]any{"type": "result", "subtype": "success", "session_id": testSession, "is_error": false, "result": "done"})
	cases := []struct {
		name, stream string
		ok           bool
	}{
		{"confirmed", init + result, true}, {"missing-init", result, false}, {"missing-result", init, false},
		{"wrong-session", init + strings.ReplaceAll(result, testSession, "another"), false},
		{"wrong-workspace", strings.ReplaceAll(init, `"cwd":`, `"wrong_cwd":`) + result, false},
		{"duplicate-result", init + result + result, false}, {"invalid-json", init + "oops\n", false},
		{"post-result-tool", init + result + line(map[string]string{"type": "tool_call", "session_id": testSession}), false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r, err := readCursor(strings.NewReader(c.stream), testSession, workspace, func() error { return nil })
			if (err == nil) != c.ok {
				t.Fatalf("result=%+v err=%v", r, err)
			}
		})
	}
}

// Large but valid responses are explicitly truncated without breaking Unicode.
func TestCursor_should_boundResult_and_rejectOversizedFrames(t *testing.T) {
	workspace := t.TempDir()
	init, _ := json.Marshal(map[string]string{"type": "system", "subtype": "init", "session_id": testSession, "cwd": workspace})
	result, _ := json.Marshal(map[string]any{"type": "result", "subtype": "success", "session_id": testSession, "is_error": false, "result": strings.Repeat("中", 30000)})
	r, err := readCursor(strings.NewReader(string(init)+"\n"+string(result)+"\n"), testSession, workspace, func() error { return nil })
	if err != nil || !r.truncated || len(r.text) > MaxResponse || strings.ContainsRune(r.text, '�') {
		t.Fatalf("truncated=%v size=%d err=%v", r.truncated, len(r.text), err)
	}
	if _, err := readCursor(strings.NewReader(strings.Repeat("x", 1024*1024+1)), testSession, workspace, func() error { return nil }); err == nil {
		t.Fatal("oversized frame accepted")
	}
}
