package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xallor-ai/xallor-remote/internal/access"
	"github.com/xallor-ai/xallor-remote/internal/accesscode"
	"github.com/xallor-ai/xallor-remote/internal/execution"
	"github.com/xallor-ai/xallor-remote/internal/ipc"
	"github.com/xallor-ai/xallor-remote/internal/job"
	"github.com/xallor-ai/xallor-remote/internal/protocol"
	"github.com/xallor-ai/xallor-remote/internal/runtimeprocess"
)

// Both Runtime owners and both task processes are independent native processes.
// Owner management must reach a real authorization_lost terminal after cleanup.
func TestIndependentRuntimeExec_shouldReapTaskTree_afterOwnerRevoke(t *testing.T) {
	assertIndependentExecManagement(t, "code.revoke")
}

// Rotation also invalidates the running task's original session. Its replacement
// code is returned as public metadata, not a new session or secret disclosure.
func TestIndependentRuntimeExec_shouldReapTaskTree_afterOwnerRotate(t *testing.T) {
	assertIndependentExecManagement(t, "code.rotate")
}

type independentTaskEvidence struct {
	Role             string `json:"role"`
	PID              int    `json:"pid"`
	ParentPID        int    `json:"parent_pid"`
	ImageBound       bool   `json:"image_bound"`
	InitiallyLive    bool   `json:"initially_live"`
	ExitedAtTerminal bool   `json:"exited_at_terminal"`
	ExitedAtCleanup  bool   `json:"exited_at_cleanup"`
}

func assertIndependentExecManagement(t *testing.T, action string) {
	t.Helper()
	h, _, device, code, session, capacity, targetID := newIndependentConsent(t, []string{protocol.OpExec})
	target, controller := h.profiles[0], h.profiles[1]
	var f ipc.Frame
	// Fresh IPC deadlines cover task readiness and shutdown, independently from
	// installation. The original controller session is shared across these IPCs.
	runner, err := h.connect(controller)
	if err != nil {
		t.Fatal(err)
	}
	executable := filepath.Join(h.bundle, h.manifest.Artifact.Filename)
	if err := ipc.Call(runner, "running-task", "exec", map[string]any{"device_id": targetID, "session_id": session.SessionID, "program": execution.Program{Executable: executable, Args: []string{"--xr-independent-exec-root"}}}); err != nil {
		t.Fatal(err)
	}
	var output strings.Builder
	tasks := map[string]*independentTaskEvidence{}
	watches := map[string]runtimeprocess.ExitWatch{}
	h.observations["task_processes"] = tasks
	h.observations["management_action"] = action
	// Retain handles until after owner shutdown even on an earlier assertion
	// failure. Never remove the profile while an observed task still lives.
	h.afterStop = append(h.afterStop, func() error {
		var result error
		for role, watch := range watches {
			exited, err := watch.Exited()
			tasks[role].ExitedAtCleanup = exited && err == nil
			if err != nil || !exited {
				result = errors.Join(result, fmt.Errorf("task %s cleanup unconfirmed", role), err)
			}
			result = errors.Join(result, watch.Close())
		}
		return result
	})
	for len(tasks) < 2 {
		f, err = runner.ReadFrame()
		if err != nil {
			t.Fatal("task readiness absent", err)
		}
		if f.ID != "running-task" || f.OK != nil {
			t.Fatal("task terminated before tree readiness", f.Code)
		}
		if f.Event != "stdout" {
			continue
		}
		output.WriteString(f.Data)
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
				t.Fatal("task image could not be independently bound", err)
			}
			watches[ready.Role] = watch
			tasks[ready.Role] = &independentTaskEvidence{Role: ready.Role, PID: ready.PID, ParentPID: ready.ParentPID, ImageBound: true}
			if exited, err := watch.Exited(); err != nil || exited {
				t.Fatal("task not alive before management", err)
			}
			tasks[ready.Role].InitiallyLive = true
		}
		if output.Len() > 16384 {
			t.Fatal("fixture readiness output exceeded bound")
		}
	}
	if tasks["root"].PID == tasks["leaf"].PID || tasks["root"].ParentPID != target.current.PID || tasks["leaf"].ParentPID != tasks["root"].PID {
		t.Fatal("task fixture parent chain incorrect")
	}
	jobs := readIndependentJobFile(t, target.dir)
	if len(jobs) != 1 || capacity.Active(device.UID) != 1 {
		t.Fatal("running task occupancy not accounted")
	}
	for _, row := range jobs {
		if row.State != "occupied" || row.Kind != string(job.KindExec) || row.Scope.DeviceUID != device.UID {
			t.Fatal("running task ledger binding incorrect")
		}
	}
	h.observations["occupied_jobs_before"] = len(jobs)
	h.observations["occupied_transport_before"] = capacity.Active(device.UID)
	mutation, err := h.connect(target)
	if err != nil {
		t.Fatal(err)
	}
	params := map[string]any{"action": action, "code_id": code.ID}
	if action == "code.rotate" {
		params["new_code_id"] = "independent-replacement"
		params["new_secret"] = "private-replacement-secret"
	}
	f, err = independentCall(mutation, "mutate", "device.access", params)
	requireIndependentOK(t, f, err)
	if action == "code.rotate" {
		var receipt access.CodeRotationResult
		if json.Unmarshal(f.Result, &receipt) != nil || receipt.PreviousCodeID != code.ID || receipt.Code.ID != "independent-replacement" || receipt.Code.Generation != 2 || receipt.Code.State != accesscode.StateActive || !receipt.Code.RequireApproval {
			t.Fatal("rotation receipt metadata invalid")
		}
		if bytes.Contains(f.Result, []byte("private-replacement-secret")) || bytes.Contains(f.Result, []byte("intent_sha256")) {
			t.Fatal("rotation public result leaked private material")
		}
	}
	var terminal ipc.Frame
	for {
		f, err = runner.ReadFrame()
		if err != nil {
			t.Fatal("revocation did not settle running execution", err)
		}
		if f.ID != "running-task" {
			t.Fatal("execution correlation changed")
		}
		if f.OK != nil {
			terminal = f
			break
		}
		if f.Event == "stdout" {
			output.WriteString(f.Data)
		}
	}
	requireIndependentOK(t, terminal, nil)
	var result struct {
		Status   string `json:"status"`
		ExitCode *int   `json:"exit_code"`
		Duration int64  `json:"duration_ms"`
	}
	if json.Unmarshal(terminal.Result, &result) != nil || result.Status != protocol.ExitAuthorizationLost || result.ExitCode == nil || result.Duration >= 15000 {
		t.Fatal("wrong real execution terminal", terminal.Code)
	}
	for role, watch := range watches {
		exited, err := watch.Exited()
		tasks[role].ExitedAtTerminal = exited && err == nil
		if err != nil || !exited {
			t.Fatal("terminal preceded confirmed task exit", role, err)
		}
	}
	if rows := readIndependentJobFile(t, target.dir); len(rows) != 0 {
		t.Fatal("terminal preceded durable job release")
	}
	if capacity.Active(device.UID) != 0 {
		t.Fatal("terminal preceded transport release")
	}
	h.observations["terminal_status"] = result.Status
	h.observations["jobs_after_terminal"] = 0
	h.observations["transport_after_terminal"] = 0
	// The same IPC must remain usable; no second terminal may masquerade as its
	// following status response. It observes service lifetime, not replayed work.
	f, err = independentCall(runner, "after-terminal", "status", map[string]any{})
	requireIndependentOK(t, f, err)
	for _, p := range h.profiles {
		if exited, err := p.current.watch.Exited(); err != nil || exited {
			t.Fatal("management stopped an entire Runtime", err)
		}
	}
	h.cases = append(h.cases, "independent_runtime_exec_parent_and_child_live", "job_and_transport_occupied", action, "authorization_lost_terminal", "kernel_task_tree_exit", "job_and_transport_released", "same_ipc_remains_usable")
}

// Observe only; opening a second job.Ledger would compete for ownership or
// perform recovery. The target owns all writes to this atomic file.
func readIndependentJobFile(t *testing.T, dir string) map[string]job.Record {
	t.Helper()
	f, err := os.Open(filepath.Join(dir, "jobs.json"))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := io.ReadAll(io.LimitReader(f, 16385))
	closeErr := f.Close()
	if err != nil || closeErr != nil || len(raw) > 16384 {
		t.Fatal("job observation failed", err, closeErr)
	}
	var disk struct {
		Version int                   `json:"version"`
		Jobs    map[string]job.Record `json:"jobs"`
	}
	if json.Unmarshal(raw, &disk) != nil || disk.Version != 1 || disk.Jobs == nil {
		t.Fatal("job observation malformed")
	}
	return disk.Jobs
}
