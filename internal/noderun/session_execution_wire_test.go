package noderun

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/xallor-ai/xallor-remote/internal/access"
	"github.com/xallor-ai/xallor-remote/internal/accesscode"
	"github.com/xallor-ai/xallor-remote/internal/account"
	"github.com/xallor-ai/xallor-remote/internal/admission"
	"github.com/xallor-ai/xallor-remote/internal/delegation"
	"github.com/xallor-ai/xallor-remote/internal/execution"
	"github.com/xallor-ai/xallor-remote/internal/identity"
	"github.com/xallor-ai/xallor-remote/internal/ipc"
	"github.com/xallor-ai/xallor-remote/internal/principal"
	"github.com/xallor-ai/xallor-remote/internal/process"
	"github.com/xallor-ai/xallor-remote/internal/protocol"
	"github.com/xallor-ai/xallor-remote/internal/relay"
	"github.com/xallor-ai/xallor-remote/internal/runtimeprocess"
	"github.com/xallor-ai/xallor-remote/internal/storage"
)

// Only the explicitly invoked fixture process runs this branch. Both processes
// outlive all observation deadlines; ordinary exit cannot pass the revoke test.
func TestSessionLeaseChild(t *testing.T) {
	if os.Getenv("XR_SESSION_LEASE_CHILD") != "1" {
		t.Skip("subprocess-only helper")
	}
	if os.Getenv("XR_SESSION_LEASE_STAGE") == "leaf" {
		fmt.Printf("XR_SESSION_TREE_LEAF=%d\n", os.Getpid())
		time.Sleep(60 * time.Second)
		os.Exit(0)
	}
	child := exec.Command(os.Args[0], "-test.run=^TestSessionLeaseChild$")
	child.Env = append(os.Environ(), "XR_SESSION_LEASE_STAGE=leaf")
	child.Stdout, child.Stderr = os.Stdout, os.Stderr
	process.HideConsole(child)
	if err := child.Start(); err != nil {
		os.Exit(2)
	}
	fmt.Printf("XR_SESSION_TREE_ROOT=%d\n", os.Getpid())
	time.Sleep(60 * time.Second)
	os.Exit(0)
}

type sessionExecutionHarness struct {
	store                            *storage.SQLStore
	client, target                   *Daemon
	targetID, uid, codeID, sessionID string
	admission                        *admission.Service
}

// Runtime objects and Relay share this test process, but use real TLS
// WebSockets, SQLite, IPC handlers and native job/process supervision.
func newSessionExecutionHarness(t *testing.T) *sessionExecutionHarness {
	t.Helper()
	return newSessionExecutionHarnessWithOwner(t, false)
}

func newSessionExecutionHarnessWithOwner(t *testing.T, owned bool) *sessionExecutionHarness {
	return newSessionOperationHarness(t, owned, []string{protocol.OpExec})
}

func newSessionOperationHarness(t *testing.T, owned bool, scope []string) *sessionExecutionHarness {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	s, err := storage.OpenSQLStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	key, err := s.ExecutionKey()
	if err != nil {
		t.Fatal(err)
	}
	limits := admission.New(admission.Limits{PerDevice: protocol.MaxDeviceRequests})
	principals, permissions := principal.NewService(s), access.NewService(s)
	hub := relay.NewHub(s, principals, permissions, delegation.NewAuthority(key, s, principals, permissions), limits, s)
	ownerToken := ""
	if owned {
		owner, err := account.New(s).CreatePrincipal(ctx, account.CreatePrincipalRequest{Name: "execution-owner", Kind: "user", TTL: time.Hour})
		if err != nil {
			t.Fatal(err)
		}
		ownerToken = owner.Token
	}
	server := httptest.NewTLSServer(relay.Handler(hub, nil))
	t.Cleanup(server.Close)
	prior := http.DefaultTransport
	http.DefaultTransport = server.Client().Transport
	t.Cleanup(func() { http.DefaultTransport = prior })
	url := "wss" + strings.TrimPrefix(server.URL, "https")
	ts, err := identity.LoadFrom(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := ts.SetConfig(url, t.TempDir(), "auto"); err != nil {
		t.Fatal(err)
	}
	grant, err := ts.IssueGrant()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ts.SetInbound(true); err != nil {
		t.Fatal(err)
	}
	cs, err := identity.LoadFrom(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := cs.SetConfig(url, t.TempDir(), "auto"); err != nil {
		t.Fatal(err)
	}
	if err := cs.AddPeer(ts.ID(), grant); err != nil {
		t.Fatal(err)
	}
	if owned {
		if err := ts.SetAccount(ownerToken); err != nil {
			t.Fatal(err)
		}
		if err := cs.SetAccount(ownerToken); err != nil {
			t.Fatal(err)
		}
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	target, client := New(ts, logger), New(cs, logger)
	connected := make(chan error, 1)
	go func() { connected <- target.connectDevice() }()
	t.Cleanup(func() {
		client.Stop()
		client.dropClient(ts.ID(), nil)
		target.Stop()
		target.waitLocalExecs()
		select {
		case <-connected:
		case <-time.After(5 * time.Second):
			t.Error("target connection did not stop")
		}
		target.handoffs.Close()
		target.jobs.Close()
		client.handoffs.Close()
		client.jobs.Close()
	})
	for {
		target.mu.Lock()
		ready := target.device != nil
		target.mu.Unlock()
		if ready {
			break
		}
		select {
		case err := <-connected:
			connected <- err
			t.Fatal("target did not authenticate", err)
		case <-ctx.Done():
			t.Fatal("target not ready")
		case <-time.After(10 * time.Millisecond):
		}
	}
	if owned {
		claim := deviceAccessIPC(t, target, access.Request{Action: "claim"})
		if claim.OK == nil || !*claim.OK {
			t.Fatal("owner claim failed", claim.Code)
		}
	}
	device, err := s.LoadDevice(ctx, ts.ID())
	if err != nil {
		t.Fatal(err)
	}
	h := &sessionExecutionHarness{store: s, client: client, target: target, targetID: ts.ID(), uid: device.UID, codeID: "wire-exec-code", sessionID: "", admission: limits}
	now := time.Now().UTC()
	code := accesscode.Code{ID: h.codeID, DeviceUID: device.UID, OwnershipID: device.OwnershipID, Type: accesscode.OneTime, Generation: 1, IssuedAt: now, Scope: append([]string(nil), scope...), RequireApproval: true, State: accesscode.StateActive}
	if owned {
		code.Type = accesscode.LongLived
	}
	if err := s.WriteAccessCode(ctx, func(tx accesscode.Transaction) error {
		return tx.InsertCode(ctx, code, protocol.SHA256Hex("wire-secret"))
	}); err != nil {
		t.Fatal(err)
	}
	approval := &testSink{ch: make(chan ipc.Frame, 1)}
	target.sessionApprovals.subscribe(approval)
	defer target.sessionApprovals.unsubscribe(approval)
	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()
	_ = b.SetDeadline(time.Now().Add(10 * time.Second))
	done := make(chan struct{})
	go func() {
		defer close(done)
		client.ipcSessionRequest(ipc.NewConn(a), ipc.Request{ID: "approve", Method: "session.request", Params: mustJSON(sessionRequestParams{DeviceID: h.targetID, TargetUID: h.uid, CodeID: h.codeID, CodeSecret: "wire-secret", Generation: 1, Scope: code.Scope, SessionExpiresAt: ptrInt64(now.Add(time.Minute).Unix())})})
	}()
	var event ipc.Frame
	select {
	case event = <-approval.ch:
	case <-ctx.Done():
		t.Fatal("session approval not delivered")
	}
	var payload struct {
		Digest string `json:"request_sha256"`
	}
	if json.Unmarshal(event.Params, &payload) != nil || !target.sessionApprovals.respond(event.ID, payload.Digest, true) {
		t.Fatal("approval not accepted")
	}
	result, err := ipc.NewConn(b).ReadFrame()
	if err != nil {
		t.Fatal(err)
	}
	<-done
	var value struct {
		SessionID string `json:"session_id"`
		Status    string `json:"status"`
	}
	if result.OK == nil || !*result.OK || json.Unmarshal(result.Result, &value) != nil || value.Status != protocol.SessionActive {
		t.Fatal("session did not activate", result.Code)
	}
	h.sessionID = value.SessionID
	return h
}

// Revocation is committed only after root and descendant are proven running.
// Real renewal then cancels the native process tree and releases the job ledger
// before authorization_lost is delivered through the original IPC stream.
func TestSessionExecutionRuntime_shouldStopRealProcessTree_afterCodeRevocation(t *testing.T) {
	assertSessionExecutionRevocation(t, "storage-revoke")
}

// Owner revoke/rotate traverses Runtime management IPC and the live TLS Relay.
// The original execution must report authorization_lost after both processes
// exit; a code-management success alone is not cleanup evidence.
func TestManagedCodeExecutionRuntime_shouldStopRealProcessTree_withAuthorizationLost(t *testing.T) {
	for _, action := range []string{"code.revoke", "code.rotate"} {
		t.Run(action, func(t *testing.T) { assertSessionExecutionRevocation(t, action) })
	}
}

func assertSessionExecutionRevocation(t *testing.T, action string) {
	t.Helper()
	t.Setenv("XR_SESSION_LEASE_CHILD", "1")
	t.Setenv("XR_SESSION_LEASE_STAGE", "")
	h := newSessionExecutionHarnessWithOwner(t, action != "storage-revoke")
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()
	_ = b.SetDeadline(time.Now().Add(30 * time.Second))
	reader := ipc.NewConn(b)
	done := make(chan struct{})
	go func() {
		defer close(done)
		h.client.ipcExec(ipc.NewConn(a), ipc.Request{ID: "run", Method: "exec", Params: mustJSON(map[string]any{"device_id": h.targetID, "session_id": h.sessionID, "program": execution.Program{Executable: self, Args: []string{"-test.run=^TestSessionLeaseChild$"}}})})
	}()
	t.Cleanup(func() {
		h.client.Stop()
		_ = a.Close()
		_ = b.Close()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("exec IPC handler did not finish")
		}
	})
	pattern := regexp.MustCompile(`(?m)^XR_SESSION_TREE_(ROOT|LEAF)=([0-9]+)\r?$`)
	pids := map[string]int{}
	var output strings.Builder
	for len(pids) < 2 {
		frame, err := reader.ReadFrame()
		if err != nil {
			t.Fatal("process tree readiness not observed", err)
		}
		if frame.OK != nil {
			t.Fatal("execution ended before process readiness", frame.Code, string(frame.Result))
		}
		output.WriteString(frame.Data)
		for _, match := range pattern.FindAllStringSubmatch(output.String(), -1) {
			pid, _ := strconv.Atoi(match[2])
			pids[match[1]] = pid
		}
	}
	watches := []runtimeprocess.ExitWatch{}
	for _, pid := range pids {
		watch, err := runtimeprocess.WatchExit(pid)
		if err != nil {
			t.Fatal(err)
		}
		defer watch.Close()
		if exited, err := watch.Exited(); err != nil || exited {
			t.Fatal("fixture process not live", err)
		}
		watches = append(watches, watch)
	}
	rows, err := h.target.jobs.Snapshot()
	if err != nil || len(rows) != 1 || rows[0].State != "occupied" || h.admission.Active(h.uid) != 1 {
		t.Fatal("real execution not accounted", rows, err)
	}
	if action == "storage-revoke" {
		if err := h.store.WriteAccessCode(context.Background(), func(tx accesscode.Transaction) error {
			_, err := tx.RevokeCode(context.Background(), h.codeID)
			return err
		}); err != nil {
			t.Fatal(err)
		}
	} else {
		req := access.Request{Action: action, CodeID: h.codeID}
		if action == "code.rotate" {
			req.NewCodeID, req.NewSecret = "replacement-exec-code", "replacement-exec-secret"
		}
		managed := deviceAccessIPC(t, h.target, req)
		if managed.OK == nil || !*managed.OK {
			t.Fatal("owner mutation not acknowledged", managed.Code)
		}
	}
	_ = b.SetDeadline(time.Now().Add(12 * time.Second))
	var terminal ipc.Frame
	for {
		frame, err := reader.ReadFrame()
		if err != nil {
			t.Fatal("revocation did not settle execution", err)
		}
		if frame.OK != nil {
			terminal = frame
			break
		}
		output.WriteString(frame.Data)
	}
	<-done
	var result struct {
		Status   string `json:"status"`
		ExitCode *int   `json:"exit_code"`
	}
	if terminal.OK == nil || !*terminal.OK || json.Unmarshal(terminal.Result, &result) != nil || result.Status != protocol.ExitAuthorizationLost || result.ExitCode == nil {
		t.Fatal("wrong terminal after revocation", terminal.Code, string(terminal.Result))
	}
	for _, watch := range watches {
		if exited, err := watch.Exited(); err != nil || !exited {
			t.Fatal("terminal preceded confirmed process exit", err)
		}
	}
	rows, err = h.target.jobs.Snapshot()
	if err != nil || len(rows) != 0 {
		t.Fatal("confirmed process cleanup did not release ledger", rows, err)
	}
	if h.admission.Active(h.uid) != 0 {
		t.Fatal("terminal did not release transport occupancy")
	}
	if !strings.Contains(output.String(), "XR_SESSION_TREE_ROOT=") || !strings.Contains(output.String(), "XR_SESSION_TREE_LEAF=") {
		t.Fatal("startup output lost")
	}
	t.Logf("native root and descendant exited; status=%s; job/transport occupancy=0", result.Status)
}
