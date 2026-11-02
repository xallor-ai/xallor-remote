package handoff

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"unicode/utf8"
)

type cursorResult struct {
	text      string
	truncated bool
	isError   bool
}

// Only matching init + matching final result establish a result. Launch, EOF,
// assistant text and an exit code alone are never delivery/completion receipts.
func readCursor(input io.Reader, session, workspace string, onInit func() error) (cursorResult, error) {
	scan := bufio.NewScanner(input)
	scan.Buffer(make([]byte, 4096), 1024*1024)
	initialized, finished := false, false
	var result cursorResult
	total := 0
	for scan.Scan() {
		line := scan.Bytes()
		total += len(line)
		if total > 8*1024*1024 {
			return result, fmt.Errorf("output_limit")
		}
		var e struct {
			Type    string  `json:"type"`
			Subtype string  `json:"subtype"`
			Session string  `json:"session_id"`
			Cwd     string  `json:"cwd"`
			IsError *bool   `json:"is_error"`
			Result  *string `json:"result"`
		}
		if json.Unmarshal(line, &e) != nil {
			return result, fmt.Errorf("invalid_cli_stream")
		}
		if e.Session != "" && e.Session != session {
			return result, fmt.Errorf("session_mismatch")
		}
		if e.Type == "system" && e.Subtype == "init" {
			if initialized || e.Session != session || !samePath(e.Cwd, workspace) {
				return result, fmt.Errorf("session_or_workspace_mismatch")
			}
			initialized = true
			if err := onInit(); err != nil {
				return result, fmt.Errorf("receipt_unavailable")
			}
		}
		if e.Type == "result" {
			if !initialized || finished || e.Session != session || e.IsError == nil || e.Result == nil {
				return result, fmt.Errorf("unconfirmed_cli_result")
			}
			if !*e.IsError && e.Subtype != "success" {
				return result, fmt.Errorf("unconfirmed_cli_result")
			}
			finished = true
			result.isError = *e.IsError
			result.text = *e.Result
			if len(result.text) > MaxResponse {
				end := MaxResponse
				for end > 0 && !utf8.RuneStart(result.text[end]) {
					end--
				}
				result.text = result.text[:end]
				result.truncated = true
			}
		}
		if finished && (e.Type == "tool_call" || e.Type == "assistant") {
			return result, fmt.Errorf("events_after_result")
		}
	}
	if scan.Err() != nil {
		return result, fmt.Errorf("invalid_cli_stream")
	}
	if !finished {
		return result, fmt.Errorf("missing_cli_result")
	}
	return result, nil
}
