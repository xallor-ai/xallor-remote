package noderun

import (
	"context"
	"encoding/json"
	"net"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/xallor-ai/xallor-remote/internal/accesscode"
	"github.com/xallor-ai/xallor-remote/internal/handoff"
	"github.com/xallor-ai/xallor-remote/internal/ipc"
	"github.com/xallor-ai/xallor-remote/internal/protocol"
)

// Real IPC framing and TLS Relay routing must distinguish approved consent
// from the nested Cursor session. Deliberate preflight refusal avoids any Agent.
func TestSessionHandoffRuntime_shouldKeepConsentSeparate_overSubmitAndReceiptQuery(t *testing.T) {
	h := newSessionOperationHarness(t, false, []string{protocol.OpHandoffSubmit, protocol.OpHandoffGet})
	_, _, _, workspace, _, _, _ := h.target.store.Snapshot()
	request := handoff.Request{ID: "consent-wire", Workspace: filepath.Join(workspace, "not-created"), Revision: strings.Repeat("a", 40), Agent: "cursor", Session: "12345678-1234-1234-1234-123456789abc", Mode: "ask", Prompt: "controlled preflight refusal", TimeoutSeconds: 10}
	for _, wrong := range []string{"", request.Session, "other-consent"} {
		params := map[string]any{"device_id": h.targetID, "request": request}
		if wrong != "" {
			params["session_id"] = wrong
		}
		f := handoffSessionFrame(t, h.client, protocol.OpHandoffSubmit, params)
		if f.OK == nil || *f.OK || f.Code != protocol.SessionPending {
			t.Fatal("missing or unrelated consent reached handoff submission", f.Code)
		}
	}
	frame := handoffSessionFrame(t, h.client, protocol.OpHandoffSubmit, map[string]any{"device_id": h.targetID, "session_id": h.sessionID, "request": request})
	receipt := handoffSessionReceipt(t, frame)
	if receipt.ID != request.ID || receipt.Session != request.Session || receipt.Session == h.sessionID || receipt.State != "received" {
		t.Fatal("submission lost request or Cursor identity", receipt)
	}
	query := func(session string) ipc.Frame {
		return handoffSessionFrame(t, h.client, protocol.OpHandoffGet, map[string]any{"device_id": h.targetID, "session_id": session, "request_id": request.ID})
	}
	for _, wrong := range []string{"", request.Session, "other-consent"} {
		f := query(wrong)
		if f.OK == nil || *f.OK || f.Code != protocol.SessionPending {
			t.Fatal("missing or unrelated consent reached receipt", f.Code)
		}
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		receipt = handoffSessionReceipt(t, query(h.sessionID))
		if receipt.ID != request.ID || receipt.Session != request.Session {
			t.Fatal("query changed identities")
		}
		if receipt.State == "failed" {
			break
		}
		if receipt.State != "received" || time.Now().After(deadline) {
			t.Fatal("controlled preflight did not settle", receipt)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if receipt.Code != "workspace_outside_root" {
		t.Fatal("unexpected preflight result", receipt)
	}
	rows, err := h.target.jobs.Snapshot()
	if err != nil || len(rows) != 0 {
		t.Fatal("failed preflight retained job capacity", err)
	}
	// Durable revocation must deny subsequent receipt access even though the
	// transport grant and the stored Cursor receipt still exist.
	if err := h.store.WriteAccessCode(context.Background(), func(tx accesscode.Transaction) error {
		_, err := tx.RevokeCode(context.Background(), h.codeID)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	f := query(h.sessionID)
	if f.OK == nil || *f.OK || f.Code != protocol.Unauthorized {
		t.Fatal("revoked consent read receipt", f.Code)
	}
}

// Approval of either handoff operation cannot authorize the other. A Relay
// denial is distinct from the target returning an application/receipt error.
func TestSessionHandoffRuntime_shouldRejectOtherOperation_whenOnlyOneScopeIsApproved(t *testing.T) {
	for _, allowed := range []string{protocol.OpHandoffSubmit, protocol.OpHandoffGet} {
		t.Run(allowed, func(t *testing.T) {
			h := newSessionOperationHarness(t, false, []string{allowed})
			denied := protocol.OpHandoffSubmit
			if allowed == denied {
				denied = protocol.OpHandoffGet
			}
			_, _, _, workspace, _, _, _ := h.target.store.Snapshot()
			request := handoff.Request{ID: "scope-wire", Workspace: filepath.Join(workspace, "not-created"), Revision: strings.Repeat("a", 40), Agent: "cursor", Session: "12345678-1234-1234-1234-123456789abc", Mode: "ask", Prompt: "controlled scope check", TimeoutSeconds: 10}
			f := handoffSessionFrame(t, h.client, denied, map[string]any{"device_id": h.targetID, "session_id": h.sessionID, "request": request, "request_id": request.ID})
			if f.OK == nil || *f.OK || f.Code != protocol.Unauthorized {
				t.Fatal("unapproved operation reached the target", allowed, denied, f.Code)
			}
			rows, err := h.target.jobs.Snapshot()
			if err != nil || len(rows) != 0 {
				t.Fatal("denied operation allocated a target job", err)
			}
		})
	}
}

func handoffSessionFrame(t *testing.T, d *Daemon, method string, params map[string]any) ipc.Frame {
	t.Helper()
	a, b := net.Pipe()
	done := make(chan struct{})
	go func() { defer close(done); d.serveIPC(ipc.NewConn(a)) }()
	defer func() {
		_ = b.Close()
		_ = a.Close()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("handoff IPC server did not stop")
		}
	}()
	_ = b.SetDeadline(time.Now().Add(10 * time.Second))
	c := ipc.NewConn(b)
	if err := ipc.Call(c, "handoff-wire", method, params); err != nil {
		t.Fatal(err)
	}
	f, err := c.ReadFrame()
	if err != nil {
		t.Fatal(err)
	}
	if f.ID != "handoff-wire" {
		t.Fatal("response correlation changed")
	}
	return f
}

func handoffSessionReceipt(t *testing.T, f ipc.Frame) handoff.Receipt {
	t.Helper()
	var terminal struct {
		Status  string `json:"status"`
		Exit    *int   `json:"exit_code"`
		Content string `json:"content"`
	}
	if f.OK == nil || !*f.OK || json.Unmarshal(f.Result, &terminal) != nil || terminal.Status != protocol.ExitCompleted || terminal.Exit == nil || *terminal.Exit != 0 {
		t.Fatal("handoff invoke failed", f.Code)
	}
	var response struct {
		Receipt handoff.Receipt `json:"receipt"`
		Error   string          `json:"error"`
	}
	if json.Unmarshal([]byte(terminal.Content), &response) != nil || response.Error != "" || response.Receipt.ID == "" {
		t.Fatal("handoff application rejected", response.Error)
	}
	return response.Receipt
}
