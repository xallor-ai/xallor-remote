package relay

import (
	"testing"

	"github.com/xallor-ai/xallor-remote/internal/delegation"
	"github.com/xallor-ai/xallor-remote/internal/protocol"
)

type settingsController struct {
	*recSender
	closed bool
}

func (s *settingsController) Close() { s.closed = true }

func toggleSessionInbound(h *Hub, target Sender, on bool) {
	h.Handle(target, protocol.RoleDevice, "device", protocol.Message{Type: protocol.TypeDeviceSettings, RequestID: "toggle", GrantHash: protocol.SHA256Hex("xr_grant_transport"), Inbound: protocol.Bool(on)})
}

func approveSession(t *testing.T, h *Hub, target *recSender, client Sender, request protocol.Message) protocol.Message {
	t.Helper()
	h.Handle(client, protocol.RoleClient, "device", request)
	approval := target.last()
	if approval.Type != protocol.TypeSessionApprovalRequest {
		t.Fatalf("approval not requested: %s %s", approval.Type, approval.Code)
	}
	h.Handle(target, protocol.RoleDevice, "device", protocol.Message{Type: protocol.TypeSessionApprovalResult, RequestID: approval.RequestID, SessionID: approval.SessionID, RequestSHA256: approval.RequestSHA256, Allow: protocol.Bool(true)})
	return approval
}

// Consent revocation cannot manufacture a terminal or release capacity. Keep
// the original stream until the target's real terminal, while denying renewal.
func TestSessionSettings_shouldKeepExecutionStreamUntilTerminal_whenConsentIsRevoked(t *testing.T) {
	s, h, target, _ := approvedSessionFixture(t)
	h.executionAuthority = delegation.NewAuthority([32]byte{1}, s, nil, nil)
	client := &settingsController{recSender: &recSender{}}
	request := nextSessionRequest(t, s, h, client, "running", 2, protocol.OpExec)
	approveSession(t, h, target, client, request)
	h.Handle(client, protocol.RoleClient, "device", protocol.Message{Type: protocol.TypeInvoke, ExecID: "running", SessionID: request.SessionID, Op: protocol.OpExec})
	route := target.last().ExecID
	uid := h.devices["device"].record.UID
	if h.inflight[route] == nil || h.admission.Active(uid) != 1 {
		t.Fatal("exec not admitted")
	}
	h.Handle(target, protocol.RoleDevice, "device", protocol.Message{Type: protocol.TypeExecutionRenewRoute, RequestID: "before", ExecID: route})
	if target.last().Code != "" {
		t.Fatal("initial lease not valid")
	}
	for _, on := range []bool{false, true} {
		toggleSessionInbound(h, target, on)
		if client.closed || h.inflight[route] == nil || h.admission.Active(uid) != 1 {
			t.Fatal("revocation lost stream or released transport capacity")
		}
	}
	h.Handle(target, protocol.RoleDevice, "device", protocol.Message{Type: protocol.TypeExecutionRenewRoute, RequestID: "after", ExecID: route})
	if target.last().Code != protocol.Unauthorized {
		t.Fatal("old lease revived after re-enable")
	}
	h.Handle(target, protocol.RoleDevice, "device", protocol.Message{Type: protocol.TypeStdout, ExecID: route, Data: "last-output"})
	if got := client.last(); got.ExecID != "running" || got.Data != "last-output" {
		t.Fatal("old output lost")
	}
	// Explicitly approve a new session on the same controller transport while
	// the old invocation still owns its route; its terminal must remain separate.
	newRequest := nextSessionRequest(t, s, h, client, "renewed", 3)
	approveSession(t, h, target, client, newRequest)
	if client.last().Status != protocol.SessionActive {
		t.Fatal("same connection could not obtain new consent")
	}
	h.Handle(target, protocol.RoleDevice, "device", protocol.Message{Type: protocol.TypeExit, ExecID: route, Status: protocol.ExitAuthorizationLost})
	if got := client.last(); got.Type != protocol.TypeExit || got.ExecID != "running" || got.Status != protocol.ExitAuthorizationLost {
		t.Fatal("real terminal lost or changed")
	}
	if h.inflight[route] != nil || h.admission.Active(uid) != 0 {
		t.Fatal("real terminal did not release its transport lease")
	}
	if client.closed {
		t.Fatal("shared controller connection closed")
	}
	h.Handle(client, protocol.RoleClient, "device", protocol.Message{Type: protocol.TypeInvoke, ExecID: "new-info", SessionID: newRequest.SessionID, Op: protocol.OpInfo})
	if target.last().Type != protocol.TypeInvoke {
		t.Fatal("new approved session unusable")
	}
}

// Denial or failed approval delivery on a retired connection cannot restore
// grant fallback. It may explicitly request consent again on the same transport.
func TestSessionSettings_shouldRequireConsentAfterFailure_whenRetiredClientReapplies(t *testing.T) {
	for _, failure := range []string{"denied", "queue-refused"} {
		t.Run(failure, func(t *testing.T) {
			s, h, target, client := approvedSessionFixture(t)
			toggleSessionInbound(h, target, false)
			toggleSessionInbound(h, target, true)
			request := nextSessionRequest(t, s, h, client, "retry", 2)
			var from Sender = target
			if failure == "queue-refused" {
				from = &targetRejectSender{recSender: target, kind: protocol.TypeSessionApprovalRequest}
				h.devices["device"].conn = from
				h.devices["device"].sessionConnection = from
			}
			h.Handle(client, protocol.RoleClient, "device", request)
			if failure == "denied" {
				approval := target.last()
				h.Handle(target, protocol.RoleDevice, "device", protocol.Message{Type: protocol.TypeSessionApprovalResult, RequestID: approval.RequestID, SessionID: approval.SessionID, RequestSHA256: approval.RequestSHA256, Allow: protocol.Bool(false)})
			}
			h.Handle(client, protocol.RoleClient, "device", protocol.Message{Type: protocol.TypeInvoke, ExecID: "fallback", Op: protocol.OpInfo})
			if got := client.last(); got.Type != protocol.TypeInvokeNack || got.Code != protocol.SessionPending {
				t.Fatal("retired connection fell back to grant")
			}
			if !h.clients[client].sessionRequired || h.clients[client].session != nil {
				t.Fatal("logical mode lost")
			}
			h.devices["device"].conn = target
			h.devices["device"].sessionConnection = target
			next := nextSessionRequest(t, s, h, client, "retry-approved", 3)
			approveSession(t, h, target, client, next)
			if client.last().Status != protocol.SessionActive {
				t.Fatal("new consent was not possible")
			}
		})
	}
}
