package relay

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/xallor-ai/xallor-remote/internal/access"
	"github.com/xallor-ai/xallor-remote/internal/accesscode"
	"github.com/xallor-ai/xallor-remote/internal/delegation"
	"github.com/xallor-ai/xallor-remote/internal/protocol"
)

// The owner action must cancel the original code's route even after this same
// controller resumes a different session. Other routes keep their live leases.
// Target frames here prove Relay routing/occupancy only, not native task exit.
func TestCodeSessionRoutes_shouldFenceOriginalConsent_afterControllerResumesAnotherCode(t *testing.T) {
	for _, action := range []string{"code.revoke", "code.rotate"} {
		for _, selected := range []string{"A", "B"} {
			t.Run(action+"/"+selected, func(t *testing.T) {
				s, h, target, client, device, token := typedCodeFixture(t)
				h.executionAuthority = delegation.NewAuthority([32]byte{1}, s, s, access.NewService(s))
				if device.UID == "device" {
					t.Fatal("fixture must distinguish UID and display ID")
				}
				first := approveManagedExecCode(t, s, h, target, client, device, "A", 2)
				h.Handle(client, protocol.RoleClient, "device", protocol.Message{Type: protocol.TypeInvoke, ExecID: "run-A", SessionID: first.SessionID, Op: protocol.OpExec})
				ra := target.last().ExecID
				other := &recSender{}
				if code := h.RegisterClient("device", "", token, other); code != "" {
					t.Fatal(code)
				}
				second := approveManagedExecCode(t, s, h, target, other, device, "B", 3)
				h.Drop(other, protocol.RoleClient, "device")
				h.Handle(client, protocol.RoleClient, "device", protocol.Message{Type: protocol.TypeSessionResumeRequest,
					RequestID: "resume-B", SessionID: second.SessionID, ResumeID: second.ResumeID,
					ResumeCapability: second.ResumeCapability, TargetUID: device.UID})
				if got := client.last(); got.Type != protocol.TypeSessionResumeResult || got.Status != protocol.SessionActive {
					t.Fatal("session B did not resume", got.Code)
				}
				h.Handle(client, protocol.RoleClient, "device", protocol.Message{Type: protocol.TypeInvoke, ExecID: "run-B", SessionID: second.SessionID, Op: protocol.OpExec})
				rb := target.last().ExecID
				if ra == rb || h.inflight[ra] == nil || h.inflight[rb] == nil || h.admission.Active(device.UID) != 2 {
					t.Fatal("two distinct executions not admitted")
				}
				for _, route := range []string{ra, rb} {
					h.Handle(target, protocol.RoleDevice, "device", protocol.Message{Type: protocol.TypeExecutionRenewRoute, RequestID: "before", ExecID: route})
					if got := target.last(); got.Code != "" || got.LeaseMS <= 0 {
						t.Fatal("baseline lease failed", got.Code)
					}
				}
				target.mu.Lock()
				before := len(target.got)
				target.mu.Unlock()
				req := access.Request{Action: action, CodeID: "code-" + selected}
				if action == "code.rotate" {
					req.NewCodeID, req.NewSecret = "replacement-"+selected, "replacement-secret"
				}
				if got := sendTypedAccess(t, h, target, token, req); got.Type != protocol.TypeAccessResult || got.Code != "" {
					t.Fatal("owner mutation failed", got.Code)
				}
				want, survivor := ra, rb
				if selected == "B" {
					want, survivor = rb, ra
				}
				if !h.inflight[want].cancelled || h.inflight[survivor].cancelled {
					t.Error("fence did not select original route consent")
				}
				target.mu.Lock()
				notifications := append([]protocol.Message(nil), target.got[before:]...)
				target.mu.Unlock()
				cancels := 0
				for _, msg := range notifications {
					if msg.Type != protocol.TypeInvoke || msg.Op != protocol.OpCancel {
						continue
					}
					cancels++
					if msg.ExecID != want || msg.Code != protocol.Unauthorized {
						t.Errorf("wrong cancel route/reason: route=%s code=%s", msg.ExecID, msg.Code)
					}
				}
				if cancels != 1 {
					t.Errorf("target received %d authorization cancels, want 1", cancels)
				}
				current := h.clients[client]
				if selected == "A" && (current.session == nil || current.session.ID != second.SessionID) {
					t.Error("unrelated current session detached")
				}
				if selected == "B" && (current.session != nil || !current.sessionRequired) {
					t.Error("revoked current session retained or grant fallback reopened")
				}
				h.Handle(target, protocol.RoleDevice, "device", protocol.Message{Type: protocol.TypeExecutionRenewRoute, RequestID: "survivor", ExecID: survivor})
				if got := target.last(); got.Code != "" || got.LeaseMS <= 0 {
					t.Error("unrelated original consent no longer renews", got.Code)
				}
				h.Handle(target, protocol.RoleDevice, "device", protocol.Message{Type: protocol.TypeExecutionRenewRoute, RequestID: "denied", ExecID: want})
				if target.last().Code != protocol.Unauthorized {
					t.Error("revoked route renewed")
				}
				if h.admission.Active(device.UID) != 2 {
					t.Error("cancel falsely released occupancy")
				}
				h.Handle(target, protocol.RoleDevice, "device", protocol.Message{Type: protocol.TypeStdout, ExecID: want, Data: "final-output"})
				if client.last().Data != "final-output" {
					t.Error("cancel lost remaining output")
				}
				h.Handle(target, protocol.RoleDevice, "device", protocol.Message{Type: protocol.TypeExit, ExecID: want, Status: protocol.ExitAuthorizationLost})
				if h.admission.Active(device.UID) != 1 || h.inflight[survivor] == nil {
					t.Error("terminal did not settle only its route")
				}
			})
		}
	}
}

func approveManagedExecCode(t *testing.T, s *relaySQLFixture, h *Hub, target, client *recSender, device access.Device, suffix string, generation uint64) protocol.Message {
	t.Helper()
	ctx := context.Background()
	code := accesscode.Code{ID: "code-" + suffix, DeviceUID: device.UID, OwnershipID: device.OwnershipID,
		Type: accesscode.LongLived, Generation: generation, IssuedAt: time.Now().UTC(), Scope: []string{protocol.OpExec}, RequireApproval: true, State: accesscode.StateActive}
	if err := s.WriteAccessCode(ctx, func(tx accesscode.Transaction) error {
		return tx.InsertCode(ctx, code, protocol.SHA256Hex("secret-"+suffix))
	}); err != nil {
		t.Fatal(err)
	}
	expires := time.Now().Add(time.Hour).Unix()
	h.Handle(client, protocol.RoleClient, "device", protocol.Message{Type: protocol.TypeSessionRequest, RequestID: "approve-" + suffix, SessionID: "session-" + suffix,
		TargetID: "device", TargetUID: device.UID, CodeID: code.ID, CodeGeneration: generation, SessionScope: code.Scope, SessionExpiresAt: &expires, Grant: "secret-" + suffix})
	approval := target.last()
	if approval.Type != protocol.TypeSessionApprovalRequest {
		t.Fatal("approval was not requested", client.last().Code)
	}
	h.Handle(target, protocol.RoleDevice, "device", protocol.Message{Type: protocol.TypeSessionApprovalResult, RequestID: approval.RequestID, SessionID: approval.SessionID, RequestSHA256: approval.RequestSHA256, Allow: protocol.Bool(true)})
	active := client.last()
	if active.Status != protocol.SessionActive {
		t.Fatal("approval failed", active.Code)
	}
	return active
}

// A controller owns ordinary cancellation but cannot impersonate a Relay
// authorization revocation by injecting an error code into its input envelope.
func TestCodeSessionRoutes_shouldStripForgedRevocationReason_fromClientCancel(t *testing.T) {
	_, h, target, client, _, route := sessionExecutionFixture(t)
	h.Handle(client, protocol.RoleClient, "device", protocol.Message{Type: protocol.TypeInvoke, Op: protocol.OpCancel, ExecID: "run", Code: protocol.Unauthorized})
	got := target.last()
	if got.Type != protocol.TypeInvoke || got.Op != protocol.OpCancel || got.ExecID != route || got.Code != "" {
		t.Fatal("controller forged authorization-loss reason")
	}
}

// Stale route state must not direct a cancellation at a replacement identity
// or epoch, and a different code generation must not poison the original route.
func TestCodeSessionRoutes_shouldKeepNotificationBound_toOriginalTargetAndGeneration(t *testing.T) {
	for _, change := range []string{"target-uid", "target-epoch", "code-generation"} {
		t.Run(change, func(t *testing.T) {
			_, h, target, client, request, route := sessionExecutionFixture(t)
			uid := h.devices["device"].record.UID
			generation := request.CodeGeneration
			switch change {
			case "target-uid":
				h.devices["device"].record.UID = strings.Repeat("c", 32)
			case "target-epoch":
				h.devices["device"].sessionTarget.Epoch = strings.Repeat("d", 32)
			case "code-generation":
				generation++
			}
			target.mu.Lock()
			before := len(target.got)
			target.mu.Unlock()
			h.mu.Lock()
			h.invalidateCodeSessionsLocked(uid, request.CodeID, generation)
			h.mu.Unlock()
			target.mu.Lock()
			after := len(target.got)
			target.mu.Unlock()
			if after != before {
				t.Fatal("cancel sent to different target/generation")
			}
			if h.inflight[route] == nil || h.admission.Active(uid) != 1 {
				t.Fatal("fence manufactured cleanup")
			}
			if change == "code-generation" {
				if h.inflight[route].cancelled || h.clients[client].session == nil {
					t.Fatal("different generation revoked live consent")
				}
			} else if !h.inflight[route].cancelled {
				t.Fatal("stale target revived revoked authority")
			}
		})
	}
}

type rejectCodeCancelSender struct {
	*recSender
	rejected bool
}

func (s *rejectCodeCancelSender) Enqueue(msg protocol.Message) error {
	if msg.Type == protocol.TypeInvoke && msg.Op == protocol.OpCancel {
		s.rejected = true
		return errors.New("test cancel queue failure")
	}
	return s.recSender.Enqueue(msg)
}

// An undelivered immediate cancel still denies future renewal but cannot
// release transport occupancy or synthesize a process-cleanup acknowledgement.
func TestCodeSessionRoutes_shouldDenyRenewal_withoutReleasingOccupancy_whenNotificationQueueFails(t *testing.T) {
	s, h, target, _, request, route := sessionExecutionFixture(t)
	ctx := context.Background()
	if err := s.WriteAccessCode(ctx, func(tx accesscode.Transaction) error { _, err := tx.RevokeCode(ctx, request.CodeID); return err }); err != nil {
		t.Fatal(err)
	}
	rejector := &rejectCodeCancelSender{recSender: target}
	h.devices["device"].conn = rejector
	uid := h.devices["device"].record.UID
	h.mu.Lock()
	h.invalidateCodeSessionsLocked(uid, request.CodeID, request.CodeGeneration)
	h.mu.Unlock()
	if !rejector.rejected {
		t.Fatal("notification failure not exercised")
	}
	h.Handle(rejector, protocol.RoleDevice, "device", protocol.Message{Type: protocol.TypeExecutionRenewRoute, RequestID: "renew", ExecID: route})
	if target.last().Code != protocol.Unauthorized || target.last().LeaseMS != 0 {
		t.Fatal("failed notification restored authority")
	}
	if h.inflight[route] == nil || h.admission.Active(uid) != 1 {
		t.Fatal("notification failure released real occupancy")
	}
}
