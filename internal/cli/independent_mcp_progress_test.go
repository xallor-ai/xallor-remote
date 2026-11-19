package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/xallor-ai/xallor-remote/internal/runtimeprocess"
)

// Consume actual MCP progress notifications until both native tasks are live.
// The image watches are independent of the reported parent/child metadata.
func (h *independentHarness) observeMCPTasks(client *independentMCPClient, token, executable string) (map[string]*independentTaskEvidence, map[string]runtimeprocess.ExitWatch, string) {
	h.t.Helper()
	tasks := map[string]*independentTaskEvidence{}
	watches := map[string]runtimeprocess.ExitWatch{}
	h.observations["task_processes"] = tasks
	h.afterStop = append(h.afterStop, func() error {
		var result error
		for role, watch := range watches {
			exited, err := watch.Exited()
			tasks[role].ExitedAtCleanup = exited && err == nil
			if err != nil || !exited {
				result = errors.Join(result, fmt.Errorf("MCP task %s cleanup unconfirmed", role), err)
			}
			result = errors.Join(result, watch.Close())
		}
		return result
	})
	deadline := time.Now().Add(15 * time.Second)
	var output strings.Builder
	execID := ""
	for len(tasks) < 2 {
		message := client.next(deadline)
		if message.ID != "" || message.Method != "notifications/progress" {
			h.t.Fatal("MCP execution ended or emitted unrelated message before task readiness")
		}
		var progress struct {
			Token   string `json:"progressToken"`
			Message string `json:"message"`
		}
		if json.Unmarshal(message.Params, &progress) != nil || progress.Token != token {
			h.t.Fatal("MCP progress correlation changed")
		}
		if execID == "" {
			if progress.Message == "" || len(progress.Message) > 128 || strings.ContainsAny(progress.Message, "{}\n\r ") {
				h.t.Fatal("MCP first progress must identify the execution")
			}
			execID = progress.Message
			continue
		}
		output.WriteString(progress.Message)
		if output.Len() > 16384 {
			h.t.Fatal("MCP fixture progress exceeded bound")
		}
		for _, line := range strings.Split(output.String(), "\n") {
			var ready struct {
				Fixture, Role string
				PID           int
				ParentPID     int `json:"parent_pid"`
			}
			if json.Unmarshal([]byte(line), &ready) != nil || ready.Fixture != "independent-exec" || (ready.Role != "root" && ready.Role != "leaf") || ready.PID <= 0 || tasks[ready.Role] != nil {
				continue
			}
			watch, err := runtimeprocess.WatchImage(ready.PID, executable, h.manifest.Artifact.SHA256)
			if err != nil {
				h.t.Fatal("MCP task image binding failed", err)
			}
			watches[ready.Role] = watch
			tasks[ready.Role] = &independentTaskEvidence{Role: ready.Role, PID: ready.PID, ParentPID: ready.ParentPID, ImageBound: true}
			if exited, err := watch.Exited(); err != nil || exited {
				h.t.Fatal("MCP task not live before mutation", err)
			}
			tasks[ready.Role].InitiallyLive = true
		}
	}
	if tasks["root"].PID == tasks["leaf"].PID || tasks["root"].ParentPID != h.profiles[0].current.PID || tasks["leaf"].ParentPID != tasks["root"].PID {
		h.t.Fatal("MCP task parent metadata mismatch")
	}
	h.observations["mcp_progress_seen"] = true
	return tasks, watches, execID
}
