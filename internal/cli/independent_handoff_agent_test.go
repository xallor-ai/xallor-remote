package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/xallor-ai/xallor-remote/internal/process"
)

// Only the copied native _test dispatcher exposes this deterministic Agent.
// It cannot naturally finish during the revocation observation window.
func runIndependentHandoffAgent(args []string) int {
	withDescendant := len(args) > 0 && args[0] == "--with-descendant"
	if withDescendant {
		args = args[1:]
	}
	if len(args) == 1 && args[0] == "--help" {
		recordIndependentAgentProbe("help")
		fmt.Println("--resume --workspace --mode --print --output-format stream-json")
		return 0
	}
	if len(args) == 1 && args[0] == "--version" {
		recordIndependentAgentProbe("version")
		fmt.Println("independent-handoff-fixture-1")
		return 0
	}
	value := func(flag string) string {
		for i := 0; i+1 < len(args); i++ {
			if args[i] == flag {
				return args[i+1]
			}
		}
		return ""
	}
	workspace, session := value("--workspace"), value("--resume")
	if workspace == "" || session == "" || value("--mode") != "ask" {
		return 2
	}
	if withDescendant {
		child := exec.Command(os.Args[0], "--xr-independent-handoff-leaf")
		process.HideConsole(child)
		child.Stdout, child.Stderr = os.Stdout, os.Stderr
		if err := child.Start(); err != nil {
			return 4
		}
		defer child.Wait()
		deadline := time.Now().Add(10 * time.Second)
		for {
			raw, err := os.ReadFile(filepath.Join(os.Getenv("XALLOR_REMOTE_DATA_DIR"), "independent-handoff-leaf-ready.json"))
			var ready struct {
				PID       int `json:"pid"`
				ParentPID int `json:"parent_pid"`
			}
			if err == nil && json.Unmarshal(raw, &ready) == nil && ready.PID == child.Process.Pid && ready.ParentPID == os.Getpid() {
				break
			}
			if time.Now().After(deadline) {
				return 5
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
	marker := filepath.Join(os.Getenv("XALLOR_REMOTE_DATA_DIR"), "independent-handoff-ready.json")
	raw, _ := json.Marshal(map[string]any{"pid": os.Getpid(), "parent_pid": os.Getppid(), "session_id": session})
	if os.WriteFile(marker+".tmp", raw, 0600) != nil || os.Rename(marker+".tmp", marker) != nil {
		return 3
	}
	_ = json.NewEncoder(os.Stdout).Encode(map[string]string{"type": "system", "subtype": "init", "session_id": session, "cwd": workspace})
	time.Sleep(90 * time.Second)
	_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"type": "result", "subtype": "success", "session_id": session, "is_error": false, "result": "natural-fixture-completion"})
	return 0
}

// The leaf inherits the Agent's normal process group/Job Object. No independent
// kill/reparenting behavior is introduced to make the supervisor test pass.
func runIndependentHandoffLeaf() int {
	path := filepath.Join(os.Getenv("XALLOR_REMOTE_DATA_DIR"), "independent-handoff-leaf-ready.json")
	raw, _ := json.Marshal(map[string]int{"pid": os.Getpid(), "parent_pid": os.Getppid()})
	if os.WriteFile(path+".tmp", raw, 0600) != nil || os.Rename(path+".tmp", path) != nil {
		return 3
	}
	time.Sleep(90 * time.Second)
	return 0
}

// File-only test diagnostics distinguish Git/lease waits from Agent probes;
// they do not alter stdout protocol or contain prompts or credentials.
func recordIndependentAgentProbe(phase string) {
	f, err := os.OpenFile(filepath.Join(os.Getenv("XALLOR_REMOTE_DATA_DIR"), "independent-agent-probes.jsonl"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return
	}
	defer f.Close()
	_ = json.NewEncoder(f).Encode(map[string]any{"phase": phase, "pid": os.Getpid(), "at": time.Now().UTC().Format(time.RFC3339Nano)})
}
