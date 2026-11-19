package cli

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/xallor-ai/xallor-remote/internal/access"
	"github.com/xallor-ai/xallor-remote/internal/accesscode"
	"github.com/xallor-ai/xallor-remote/internal/handoff"
	"github.com/xallor-ai/xallor-remote/internal/ipc"
	"github.com/xallor-ai/xallor-remote/internal/process"
	"github.com/xallor-ai/xallor-remote/internal/protocol"
	"github.com/xallor-ai/xallor-remote/internal/runtimeprocess"
)

// Independent owner processes approve handoff through OS IPC. Only after the
// real Agent is live does owner revocation invalidate its original consent.
func TestIndependentRuntimeHandoff_shouldReapAgent_afterOwnerRevokesConsent(t *testing.T) {
	assertIndependentHandoffInvalidation(t, "code.revoke")
}

// A replacement code does not lend its authority to the original running task.
func TestIndependentRuntimeHandoff_shouldReapAgent_afterOwnerRotatesCode(t *testing.T) {
	assertIndependentHandoffInvalidation(t, "code.rotate")
}

// Expiry uses the real approved deadline and clock, without a storage mutation,
// process kill, Runtime stop or synthetic authorization-loss event.
func TestIndependentRuntimeHandoff_shouldReapAgent_afterConsentNaturallyExpires(t *testing.T) {
	assertIndependentHandoffInvalidation(t, "expiry")
}

// Public CLI subprocesses submit/query with the approved consent and must
// reuse the same controller Runtime; revoked access cannot return a receipt.
func TestIndependentRuntimeHandoff_shouldUseApprovedConsent_fromPublicCLI(t *testing.T) {
	assertIndependentHandoffInvalidation(t, "code.revoke", independentHandoffOptions{publicCLI: true})
}

// A live descendant must be observed and reaped, not inferred from root exit.
func TestIndependentRuntimeHandoff_shouldReapDescendant_afterOwnerRevokesConsent(t *testing.T) {
	assertIndependentHandoffInvalidation(t, "code.revoke", independentHandoffOptions{descendant: true})
}

type independentHandoffOptions struct{ publicCLI, descendant bool }

func assertIndependentHandoffInvalidation(t *testing.T, action string, options ...independentHandoffOptions) {
	t.Helper()
	var option independentHandoffOptions
	if len(options) > 0 {
		option = options[0]
	}
	viaCLI := option.publicCLI
	source, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	var workspace, revision, executable string
	prepare := func(h *independentHarness) {
		target := h.profiles[0]
		workspace = filepath.Join(target.dir, "workspace")
		git := func(args ...string) string {
			t.Helper()
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, "git", args...)
			process.HideConsole(cmd)
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("owned Git fixture failed: %v %s", err, out)
			}
			return strings.TrimSpace(string(out))
		}
		revision = git("-C", source, "rev-parse", "HEAD")
		git("clone", "--quiet", "--no-hardlinks", "--no-checkout", "--single-branch", "--no-tags", source, workspace)
		git("-C", workspace, "checkout", "--quiet", "--detach", revision)
		if git("-C", workspace, "status", "--porcelain") != "" {
			t.Fatal("fixture workspace dirty")
		}
		executable = filepath.Join(h.bundle, h.manifest.Artifact.Filename)
		prefix := []string{"--xr-independent-handoff-agent"}
		if option.descendant {
			prefix = append(prefix, "--with-descendant")
		}
		if err := handoff.Configure(target.dir, handoff.Config{Executable: executable, Prefix: prefix}); err != nil {
			t.Fatal(err)
		}
	}
	ttl := 5 * time.Minute
	if action == "expiry" {
		ttl = 45 * time.Second
	}
	h, store, device, code, session, capacity, targetID := newIndependentConsentWith(t, []string{protocol.OpHandoffSubmit, protocol.OpHandoffGet}, ttl, prepare)
	target, controller := h.profiles[0], h.profiles[1]
	if len(h.profiles) != 3 {
		t.Fatal("independent owner observer missing")
	}
	observer := h.profiles[2]
	observe := func(c *ipc.Conn) handoff.Receipt {
		t.Helper()
		frame, err := independentCall(c, "owner-observe", protocol.OpHandoffGet, map[string]any{"device_id": targetID, "request_id": "consent-agent"})
		return independentHandoffReceipt(t, frame, err)
	}
	request := handoff.Request{ID: "consent-agent", Workspace: workspace, Revision: revision, Agent: "cursor", Session: "12345678-1234-1234-1234-123456789abc", Mode: "ask", Prompt: "controlled consent cancellation", TimeoutSeconds: 120}
	caller, err := h.connect(controller)
	if err != nil {
		t.Fatal(err)
	}
	var f ipc.Frame
	var receipt handoff.Receipt
	var requestPath string
	if viaCLI {
		requestPath = filepath.Join(h.root, "handoff-request.json")
		raw, marshalErr := json.Marshal(request)
		if marshalErr != nil {
			t.Fatal(marshalErr)
		}
		if err := os.WriteFile(requestPath, raw, 0600); err != nil {
			t.Fatal(err)
		}
		out, _, runErr := h.runPublicHead(controller, "handoff", "submit", "--device", targetID, "--session-id", session.SessionID, "--request", requestPath)
		if runErr != nil || json.Unmarshal(out, &receipt) != nil {
			t.Fatal("public handoff submit failed", runErr)
		}
		after, readErr := os.ReadFile(requestPath)
		if readErr != nil || string(after) != string(raw) {
			t.Fatal("public head modified request file", readErr)
		}
	} else {
		f, err = independentCall(caller, "handoff", protocol.OpHandoffSubmit, map[string]any{"device_id": targetID, "session_id": session.SessionID, "request": request})
		receipt = independentHandoffReceipt(t, f, err)
	}
	if receipt.ID != request.ID || receipt.Session != request.Session || receipt.State != "received" {
		t.Fatal("handoff receipt identity mismatch")
	}
	var ready struct {
		PID       int    `json:"pid"`
		ParentPID int    `json:"parent_pid"`
		Session   string `json:"session_id"`
	}
	deadline := time.Now().Add(15 * time.Second)
	for {
		raw, readErr := os.ReadFile(filepath.Join(target.dir, "independent-handoff-ready.json"))
		if readErr == nil && json.Unmarshal(raw, &ready) == nil && ready.PID > 0 {
			break
		}
		if time.Now().After(deadline) {
			observation, err := h.connect(observer)
			if err != nil {
				t.Fatal(err)
			}
			receipt := observe(observation)
			_ = observation.Close()
			probes, _ := os.ReadFile(filepath.Join(target.dir, "independent-agent-probes.jsonl"))
			if len(probes) > 4096 {
				probes = probes[:4096]
			}
			h.observations["readiness_failure_receipt_state"] = receipt.State
			h.observations["readiness_failure_receipt_code"] = receipt.Code
			h.observations["readiness_failure_agent_probes"] = string(probes)
			h.observations["readiness_failure_jobs"] = len(readIndependentJobFile(t, target.dir))
			t.Fatal("Agent not ready", receipt.State, receipt.Code, "adapter probe events", string(probes))
		}
		time.Sleep(20 * time.Millisecond)
	}
	watch, err := runtimeprocess.WatchImage(ready.PID, executable, h.manifest.Artifact.SHA256)
	if err != nil {
		t.Fatal(err)
	}
	terminalObserved := false
	h.afterStop = append(h.afterStop, func() error {
		exited, err := watch.Exited()
		h.observations["agent_exited_at_cleanup"] = exited && err == nil
		if !exited {
			err = errors.Join(err, errors.New("Agent cleanup unconfirmed"))
		}
		return errors.Join(err, watch.Close())
	})
	h.afterStop = append(h.afterStop, func() error {
		if !terminalObserved {
			return nil
		}
		if !target.current.KernelExit || !target.current.Reaped || !target.current.EndpointAbsent {
			return errors.New("target not stopped before durable receipt audit")
		}
		stored, err := readStoppedHandoffReceipt(target.dir, request.ID)
		if err != nil {
			return err
		}
		if stored.State != "unknown" || stored.Code != "execution_authorization_lost" || stored.Session != request.Session {
			return errors.New("stopped receipt differs from IPC terminal")
		}
		h.observations["durable_receipt_after_stop"] = true
		return nil
	})
	if exited, err := watch.Exited(); err != nil || exited || ready.ParentPID != target.current.PID || ready.Session != request.Session {
		t.Fatal("Agent live/image/parent/session binding failed", err)
	}
	query, err := h.connect(controller)
	if err != nil {
		t.Fatal(err)
	}
	deadline = time.Now().Add(10 * time.Second)
	for {
		if viaCLI {
			out, _, runErr := h.runPublicHead(controller, "handoff", "get", "--device", targetID, "--session-id", session.SessionID, "--request-id", request.ID)
			if runErr != nil || json.Unmarshal(out, &receipt) != nil {
				t.Fatal("public handoff get failed", runErr)
			}
		} else {
			f, err = independentCall(query, "running-receipt", protocol.OpHandoffGet, map[string]any{"device_id": targetID, "session_id": session.SessionID, "request_id": request.ID})
			receipt = independentHandoffReceipt(t, f, err)
		}
		if receipt.ID != request.ID || receipt.Session != request.Session {
			t.Fatal("running receipt identity changed")
		}
		if receipt.State == "running" {
			break
		}
		if (receipt.State != "received" && receipt.State != "accepted") || time.Now().After(deadline) {
			t.Fatal("Runtime did not parse Agent init", receipt.State, receipt.Code)
		}
		time.Sleep(20 * time.Millisecond)
	}
	h.observations["running_receipt_before_revoke"] = true
	var descendant runtimeprocess.ExitWatch
	if option.descendant {
		descendant = observeIndependentHandoffDescendant(t, h, target, executable, ready.PID)
	}
	jobs := readIndependentJobFile(t, target.dir)
	if len(jobs) != 1 {
		t.Fatal("handoff job not occupied")
	}
	for _, row := range jobs {
		if row.State != "occupied" || row.Kind != "handoff" || row.Scope.DeviceUID != device.UID {
			t.Fatal("handoff job binding wrong")
		}
	}
	h.observations["agent_pid"] = ready.PID
	h.observations["agent_image_bound"] = true
	h.observations["agent_initially_live"] = true
	h.observations["job_before_revoke"] = len(jobs)
	h.observations["invalidation_action"] = action
	h.observations["approved_expires_at"] = *session.ExpiresAt
	if action == "expiry" {
		remaining := time.Until(time.Unix(*session.ExpiresAt, 0))
		if remaining < 5*time.Second {
			t.Fatal("Agent was not observed running sufficiently before expiry")
		}
		h.observations["remaining_ms_when_running"] = remaining.Milliseconds()
	} else {
		mutation, err := h.connect(target)
		if err != nil {
			t.Fatal(err)
		}
		params := map[string]any{"action": action, "code_id": code.ID}
		if action == "code.rotate" {
			params["new_code_id"] = "handoff-replacement"
			params["new_secret"] = "private-handoff-replacement"
		}
		f, err = independentCall(mutation, "mutate", "device.access", params)
		requireIndependentOK(t, f, err)
		if action == "code.rotate" {
			var rotated access.CodeRotationResult
			if json.Unmarshal(f.Result, &rotated) != nil || rotated.PreviousCodeID != code.ID || rotated.Code.ID != "handoff-replacement" || rotated.Code.Generation != 2 || rotated.Code.State != accesscode.StateActive {
				t.Fatal("replacement code receipt mismatch")
			}
		}
	}
	// A separate owner connection can query the same principal's receipt, but
	// cannot lend authority to the original signed consent. Do not poll live files.
	deadline = time.Now().Add(30 * time.Second)
	if action == "expiry" {
		deadline = time.Unix(*session.ExpiresAt, 0).Add(10 * time.Second)
	}
	observation, err := h.connectUntil(observer, deadline)
	if err != nil {
		t.Fatal(err)
	}
	for {
		receipt = observe(observation)
		if receipt.ID != request.ID || receipt.Session != request.Session {
			t.Fatal("observer receipt identity changed")
		}
		if receipt.State == "unknown" {
			break
		}
		if receipt.State == "completed" || receipt.State == "failed" || time.Now().After(deadline) {
			t.Fatal("revocation did not settle Agent", receipt.State, receipt.Code)
		}
		time.Sleep(100 * time.Millisecond)
	}
	if receipt.Code != "execution_authorization_lost" || receipt.Session != request.Session {
		t.Fatal("wrong consent-loss receipt", receipt.Code)
	}
	terminalObserved = true
	h.observations["terminal_observed_over_owner_ipc"] = true
	if action == "expiry" {
		// Conservative request-start lease accounting may stop slightly before
		// wall expiry; it must not stop an otherwise healthy task many seconds early.
		if time.Now().Before(time.Unix(*session.ExpiresAt, 0).Add(-2 * time.Second)) {
			t.Fatal("task lost authority before natural deadline")
		}
		if err := store.ReadAccessCode(context.Background(), func(r accesscode.Reader) error {
			current, err := r.Code(context.Background(), code.ID)
			if err == nil && current.State != accesscode.StateActive {
				t.Error("expiry test mutated or consumed long-lived code")
			}
			return err
		}); err != nil {
			t.Fatal(err)
		}
	}
	if exited, err := watch.Exited(); err != nil || !exited {
		t.Fatal("receipt preceded Agent exit", err)
	}
	if descendant != nil {
		if exited, err := descendant.Exited(); err != nil || !exited {
			t.Fatal("receipt preceded descendant exit", err)
		}
		h.observations["descendant_exited_at_terminal"] = true
		h.cases = append(h.cases, "handoff_descendant_kernel_exit")
	}
	if len(readIndependentJobFile(t, target.dir)) != 0 || capacity.Active(device.UID) != 0 {
		t.Fatal("receipt preceded capacity release")
	}
	denied, err := h.connect(controller)
	if err != nil {
		t.Fatal(err)
	}
	f, err = independentCall(denied, "denied-query", protocol.OpHandoffGet, map[string]any{"device_id": targetID, "session_id": session.SessionID, "request_id": request.ID})
	if err != nil || f.OK == nil || *f.OK || (f.Code != protocol.SessionPending && f.Code != protocol.Unauthorized) {
		t.Fatal("revoked caller retrieved handoff", f.Code, err)
	}
	if viaCLI {
		out, diagnostic, runErr := h.runPublicHead(controller, "handoff", "get", "--device", targetID, "--session-id", session.SessionID, "--request-id", request.ID)
		var exit *exec.ExitError
		if !errors.As(runErr, &exit) || exit.ExitCode() != 72 || len(out) != 0 || (!strings.Contains(diagnostic, ipc.Human(protocol.SessionPending)) && !strings.Contains(diagnostic, ipc.Human(protocol.Unauthorized))) {
			t.Fatal("public head returned revoked receipt", runErr)
		}
		out, _, runErr = h.runPublicHead(controller, "status", "--json")
		var status struct {
			Build struct {
				PID    int    `json:"pid"`
				Launch string `json:"launch_id"`
				SHA    string `json:"sha256"`
			} `json:"build"`
		}
		if runErr != nil || json.Unmarshal(out, &status) != nil || status.Build.PID != controller.current.PID || status.Build.Launch != controller.current.Launch || status.Build.SHA != controller.current.SHA256 {
			t.Fatal("public head failed to reuse controller owner", runErr)
		}
		h.cases = append(h.cases, "public_cli_submit_get", "public_cli_revoked_receipt_denied", "public_cli_reuses_same_owner")
	}
	for _, p := range h.profiles {
		if exited, err := p.current.watch.Exited(); err != nil || exited {
			t.Fatal("revocation stopped Runtime", err)
		}
	}
	h.observations["terminal_state"] = receipt.State
	h.observations["terminal_code"] = receipt.Code
	h.observations["agent_exited_at_terminal"] = true
	h.observations["jobs_after_terminal"] = 0
	h.cases = append(h.cases, "recipient_consent_over_os_ipc", "independent_handoff_agent_live", action, "original_consent_lease_denied", "durable_unknown_after_agent_exit", "job_release", "invalidated_receipt_access_denied")
}

func independentHandoffReceipt(t *testing.T, f ipc.Frame, err error) handoff.Receipt {
	t.Helper()
	requireIndependentOK(t, f, err)
	var terminal struct {
		Status  string `json:"status"`
		Exit    *int   `json:"exit_code"`
		Content string `json:"content"`
	}
	if json.Unmarshal(f.Result, &terminal) != nil || terminal.Status != protocol.ExitCompleted || terminal.Exit == nil || *terminal.Exit != 0 {
		t.Fatal("handoff call not confirmed")
	}
	var response struct {
		Receipt handoff.Receipt `json:"receipt"`
		Error   string          `json:"error"`
	}
	if json.Unmarshal([]byte(terminal.Content), &response) != nil || response.Error != "" {
		t.Fatal("handoff rejected", response.Error)
	}
	return response.Receipt
}

// Called only after target shutdown. No test handle overlaps Runtime writes.
func readStoppedHandoffReceipt(dir, id string) (handoff.Receipt, error) {
	paths, err := filepath.Glob(filepath.Join(dir, "handoff", "requests", "*.json"))
	if err != nil {
		return handoff.Receipt{}, err
	}
	for _, path := range paths {
		f, err := os.Open(path)
		if err != nil {
			return handoff.Receipt{}, err
		}
		raw, err := io.ReadAll(io.LimitReader(f, 512*1024+1))
		closeErr := f.Close()
		if err != nil || closeErr != nil || len(raw) > 512*1024 {
			return handoff.Receipt{}, errors.Join(errors.New("receipt observation failed"), err, closeErr)
		}
		var row struct {
			Receipt handoff.Receipt `json:"receipt"`
		}
		if json.Unmarshal(raw, &row) != nil {
			return handoff.Receipt{}, errors.New("receipt malformed")
		}
		if row.Receipt.ID == id {
			return row.Receipt, nil
		}
	}
	return handoff.Receipt{}, errors.New("receipt missing")
}
