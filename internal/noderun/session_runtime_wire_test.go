package noderun

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/xallor-ai/xallor-remote/internal/access"
	"github.com/xallor-ai/xallor-remote/internal/accesscode"
	"github.com/xallor-ai/xallor-remote/internal/admission"
	"github.com/xallor-ai/xallor-remote/internal/identity"
	"github.com/xallor-ai/xallor-remote/internal/ipc"
	"github.com/xallor-ai/xallor-remote/internal/protocol"
	"github.com/xallor-ai/xallor-remote/internal/relay"
	"github.com/xallor-ai/xallor-remote/internal/storage"
)

// Two real Runtime daemons must complete Relay authentication, recipient
// approval and a scoped info invoke using the session returned by redemption.
func TestSessionRuntime_shouldRouteApprovedScope_overRealRuntimeRelay(t *testing.T) {
	store, err := storage.OpenSQLStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	hub := relay.NewHub(store, nil, nil, nil, admission.New(admission.Limits{PerDevice: protocol.MaxDeviceRequests}), store)
	server := httptest.NewServer(relay.Handler(hub, nil))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	relayURL := "ws" + strings.TrimPrefix(server.URL, "http")
	workspace := t.TempDir()
	targetStore, err := identity.LoadFrom(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := targetStore.SetConfig(relayURL, workspace, "auto"); err != nil {
		t.Fatal(err)
	}
	grant, err := targetStore.IssueGrant()
	if err != nil {
		t.Fatal(err)
	}
	clientStore, err := identity.LoadFrom(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := clientStore.SetConfig(relayURL, t.TempDir(), "auto"); err != nil {
		t.Fatal(err)
	}
	if err := clientStore.AddPeer(targetStore.ID(), grant); err != nil {
		t.Fatal(err)
	}

	target := New(targetStore, slog.New(slog.NewTextHandler(io.Discard, nil)))
	client := New(clientStore, slog.New(slog.NewTextHandler(io.Discard, nil)))
	defer target.Stop()
	defer client.Stop()

	approval := &testSink{ch: make(chan ipc.Frame, 1)}
	target.sessionApprovals.subscribe(approval)
	defer target.sessionApprovals.unsubscribe(approval)
	targetDone := make(chan error, 1)
	go func() { targetDone <- target.connectDevice() }()

	var targetRecord *access.Device
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		record, loadErr := store.LoadDevice(ctx, targetStore.ID())
		if loadErr == nil && record.UID != "" {
			targetRecord = &record
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if targetRecord == nil {
		t.Fatal("target Runtime did not become online")
	}

	secret := "runtime-wire-session-secret"
	now := time.Now().UTC()
	code := accesscode.Code{ID: "runtime-wire-code", DeviceUID: targetRecord.UID, Type: accesscode.OneTime, Generation: 1, IssuedAt: now, Scope: []string{protocol.OpInfo, protocol.OpWrite}, RequireApproval: true, State: accesscode.StateActive}
	if err := store.WriteAccessCode(ctx, func(tx accesscode.Transaction) error {
		return tx.InsertCode(ctx, code, protocol.SHA256Hex(secret))
	}); err != nil {
		t.Fatal(err)
	}

	peer := identity.Peer{DeviceID: targetStore.ID(), Grant: grant}
	if _, _, err := client.ensureClient(peer); err != nil {
		t.Fatal("client Runtime did not authenticate", err)
	}

	params, _ := json.Marshal(sessionRequestParams{DeviceID: peer.DeviceID, CodeID: code.ID, CodeSecret: secret, TargetUID: targetRecord.UID, Generation: 1, Scope: []string{protocol.OpInfo}, SessionExpiresAt: ptrInt64(now.Add(time.Minute).Unix())})
	serverConn, clientConn := net.Pipe()
	defer serverConn.Close()
	defer clientConn.Close()
	serverIPC := ipc.NewConn(serverConn)
	clientIPC := ipc.NewConn(clientConn)
	done := make(chan struct{})
	go func() {
		client.ipcSessionRequest(serverIPC, ipc.Request{ID: "session-request", Method: "session.request", Params: params})
		close(done)
	}()

	frame := <-approval.ch
	var approvalParams struct {
		RequestSHA256 string `json:"request_sha256"`
	}
	if err := json.Unmarshal(frame.Params, &approvalParams); err != nil || approvalParams.RequestSHA256 == "" {
		t.Fatalf("invalid approval event: %+v %v", frame, err)
	}
	if !target.sessionApprovals.respond(frame.ID, approvalParams.RequestSHA256, true) {
		t.Fatal("Runtime approval response was not accepted")
	}
	result, err := clientIPC.ReadFrame()
	if err != nil {
		t.Fatal(err)
	}
	if result.OK == nil || !*result.OK {
		t.Fatalf("session request failed: %+v", result)
	}
	var sessionResult struct {
		SessionID        string   `json:"session_id"`
		Status           string   `json:"status"`
		Generation       uint64   `json:"generation"`
		Scope            []string `json:"scope"`
		SessionExpiresAt *int64   `json:"session_expires_at"`
		ResumeID         string   `json:"resume_id"`
		ResumeCapability string   `json:"resume_capability"`
	}
	if err := json.Unmarshal(result.Result, &sessionResult); err != nil || sessionResult.SessionID == "" || sessionResult.Status != protocol.SessionActive || sessionResult.Generation != 1 || len(sessionResult.Scope) != 1 || sessionResult.Scope[0] != protocol.OpInfo || sessionResult.SessionExpiresAt == nil || sessionResult.ResumeID == "" || sessionResult.ResumeCapability == "" {
		t.Fatalf("invalid session result: %s %v", result.Result, err)
	}
	<-done

	infoParams, _ := json.Marshal(map[string]string{"device_id": peer.DeviceID, "session_id": sessionResult.SessionID})
	infoDone := make(chan struct{})
	go func() {
		client.ipcOp(serverIPC, ipc.Request{ID: "scoped-info", Method: "info", Params: infoParams}, protocol.OpInfo)
		close(infoDone)
	}()
	info, err := clientIPC.ReadFrame()
	if err != nil {
		t.Fatal(err)
	}
	if info.OK == nil || !*info.OK {
		t.Fatalf("scoped info failed: %+v", info)
	}
	<-infoDone

	// A valid resume capability keeps the session durable across the client
	// transport drop; the new link consumes and rotates it exactly once.
	client.dropClient(peer.DeviceID, nil)
	// Exercise credential rejection through Runtime IPC and live WebSocket
	// routing before the valid capability is consumed. No secrets enter failures.
	badServer, badClient := net.Pipe()
	defer badServer.Close()
	defer badClient.Close()
	_ = badClient.SetDeadline(time.Now().Add(10 * time.Second))
	badDone := make(chan struct{})
	go func() {
		defer close(badDone)
		client.ipcSessionResume(ipc.NewConn(badServer), ipc.Request{ID: "bad-resume", Method: "session.resume", Params: mustJSON(sessionResumeParams{DeviceID: peer.DeviceID, SessionID: sessionResult.SessionID, ResumeID: sessionResult.ResumeID, ResumeCapability: "wrong-capability", TargetUID: targetRecord.UID})})
	}()
	bad, err := ipc.NewConn(badClient).ReadFrame()
	if err != nil || bad.OK == nil || *bad.OK || bad.Code != protocol.Unauthorized {
		t.Fatalf("wrong capability was not rejected: code=%s err=%v", bad.Code, err)
	}
	<-badDone
	resumeServer, resumeClient := net.Pipe()
	defer resumeServer.Close()
	defer resumeClient.Close()
	resumeDone := make(chan struct{})
	go func() {
		client.ipcSessionResume(ipc.NewConn(resumeServer), ipc.Request{ID: "session-resume", Method: "session.resume", Params: mustJSON(sessionResumeParams{DeviceID: peer.DeviceID, SessionID: sessionResult.SessionID, ResumeID: sessionResult.ResumeID, ResumeCapability: sessionResult.ResumeCapability, TargetUID: targetRecord.UID})})
		close(resumeDone)
	}()
	resumed, err := ipc.NewConn(resumeClient).ReadFrame()
	if err != nil || resumed.OK == nil || !*resumed.OK {
		t.Fatalf("resume failed: %+v err=%v", resumed, err)
	}
	var resumedResult struct {
		ResumeID string `json:"resume_id"`
	}
	if err := json.Unmarshal(resumed.Result, &resumedResult); err != nil || resumedResult.ResumeID == "" || resumedResult.ResumeID == sessionResult.ResumeID {
		t.Fatalf("resume did not rotate capability: %s %v", resumed.Result, err)
	}
	<-resumeDone

	// The code permits writes, but the recipient approved info only. Restoring
	// the session must not restore the code's broader scope or create a file.
	writeDone := make(chan struct{})
	go func() {
		defer close(writeDone)
		client.ipcOp(serverIPC, ipc.Request{ID: "unapproved-write", Method: "write", Params: mustJSON(map[string]string{"device_id": peer.DeviceID, "session_id": sessionResult.SessionID, "path": "unapproved.txt", "content": "must-not-write"})}, protocol.OpWrite)
	}()
	denied, err := clientIPC.ReadFrame()
	if err != nil || denied.OK == nil || *denied.OK || denied.Code != protocol.Unauthorized {
		t.Fatalf("resumed session exceeded recipient scope: code=%s err=%v", denied.Code, err)
	}
	<-writeDone
	if _, err := os.Stat(filepath.Join(workspace, "unapproved.txt")); !os.IsNotExist(err) {
		t.Fatal("unapproved write reached workspace")
	}

	// The consumed capability cannot be replayed through the real Runtime IPC.
	reuseServer, reuseClient := net.Pipe()
	defer reuseServer.Close()
	defer reuseClient.Close()
	reuseDone := make(chan struct{})
	go func() {
		client.ipcSessionResume(ipc.NewConn(reuseServer), ipc.Request{ID: "session-reuse", Method: "session.resume", Params: mustJSON(sessionResumeParams{DeviceID: peer.DeviceID, SessionID: sessionResult.SessionID, ResumeID: sessionResult.ResumeID, ResumeCapability: sessionResult.ResumeCapability, TargetUID: targetRecord.UID})})
		close(reuseDone)
	}()
	reused, err := ipc.NewConn(reuseClient).ReadFrame()
	if err != nil || reused.OK == nil || *reused.OK {
		t.Fatalf("consumed capability was replayable: %+v err=%v", reused, err)
	}
	<-reuseDone

	cancel()
	target.Stop()
	select {
	case <-targetDone:
	case <-time.After(3 * time.Second):
		t.Fatal("target Runtime did not stop")
	}
}

func ptrInt64(value int64) *int64 { return &value }

func mustJSON(value any) []byte {
	raw, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return raw
}
