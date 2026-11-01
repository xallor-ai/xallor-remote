// Deterministic CLI contract fixture. Live Cursor validation is a separate gate.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

func main() {
	args := os.Args[1:]
	if len(args) == 1 && args[0] == "--help" {
		fmt.Println("--resume --workspace --mode --print --output-format stream-json")
		return
	}
	if len(args) == 1 && args[0] == "--version" {
		fmt.Println("fixture-1")
		return
	}
	value := func(key string) string {
		for i := 0; i+1 < len(args); i++ {
			if args[i] == key {
				return args[i+1]
			}
		}
		return ""
	}
	workspace, session := value("--workspace"), value("--resume")
	if workspace == "" || session == "" {
		os.Exit(2)
	}
	count := filepath.Join(os.Getenv("XALLOR_REMOTE_DATA_DIR"), "fixture-agent-count")
	f, err := os.OpenFile(count, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		panic(err)
	}
	_, _ = f.WriteString(session + "\n")
	_ = f.Close()
	_ = os.WriteFile(filepath.Join(os.Getenv("XALLOR_REMOTE_DATA_DIR"), "fixture-agent-pid"), []byte(strconv.Itoa(os.Getpid())), 0600)
	emit := func(v any) { b, _ := json.Marshal(v); fmt.Println(string(b)) }
	prompt := args[len(args)-1]
	expectedMode := "ask"
	if strings.Contains(prompt, "TEST_MODE_PLAN") {
		expectedMode = "plan"
	}
	if strings.Contains(prompt, "TEST_MODE_AGENT") {
		expectedMode = ""
	}
	if value("--mode") != expectedMode {
		os.Exit(3)
	}
	for _, arg := range args {
		if arg == "--force" || arg == "--approve-mcps" {
			os.Exit(4)
		}
	}
	if strings.Contains(prompt, "TEST_WRONG_SESSION") {
		session = "00000000-0000-0000-0000-000000000000"
	}
	emit(map[string]string{"type": "system", "subtype": "init", "session_id": session, "cwd": workspace})
	if strings.Contains(prompt, "TEST_WAIT") {
		time.Sleep(60 * time.Second)
	}
	if strings.Contains(prompt, "TEST_MISSING_RESULT") {
		return
	}
	response := "handoff-fixture-ok"
	if strings.Contains(prompt, "TEST_LARGE_RESULT") {
		response = strings.Repeat("中", 30000)
	}
	emit(map[string]any{"type": "result", "subtype": "success", "session_id": session, "is_error": strings.Contains(prompt, "TEST_ERROR_RESULT"), "result": response})
	if strings.Contains(prompt, "TEST_NONZERO_EXIT") {
		os.Exit(9)
	}
}
