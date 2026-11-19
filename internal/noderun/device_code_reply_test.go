package noderun

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/xallor-ai/xallor-remote/internal/access"
	"github.com/xallor-ai/xallor-remote/internal/invocation"
	"github.com/xallor-ai/xallor-remote/internal/ipc"
	"github.com/xallor-ai/xallor-remote/internal/protocol"
)

// Intent hash is independently calculated from the documented canonical JSON
// for original/replacement/SHA256("new-secret"), rather than the parser helper.
const rotationReplyFixture = `{"previous_code_id":"original","intent_sha256":"393a63eac8f1c65901a0f5c79f626fbbce0f3b3f61ac1eb665e7737be6daadb9","code":{"code_id":"replacement","device_uid":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb","ownership_id":"cccccccccccccccccccccccccccccccc","type":"long_lived","generation":2,"issued_at":"2026-01-01T00:00:00Z","scope":["info"],"require_recipient_approval":true,"state":"active"}}`

// A successful envelope cannot acknowledge a different action, code, secret or
// malformed metadata. Missing execution context remains a connection failure.
func TestDeviceCodeReply_shouldKeepMalformedAcknowledgementsUnknown(t *testing.T) {
	for _, tc := range []struct {
		name, action, payload string
		missingContext        bool
	}{
		{"ownership_instead_of_revoke", "code.revoke", `{"device_uid":"other-device","shares":[]}`, false},
		{"ownership_instead_of_rotate", "code.rotate", `{"device_uid":"other-device","shares":[]}`, false},
		{"wrong_revoked_code", "code.revoke", `{"code_id":"other-code","state":"revoked"}`, false},
		{"active_instead_of_revoked", "code.revoke", `{"code_id":"original","state":"active"}`, false},
		{"secret_in_revoke", "code.revoke", `{"code_id":"original","state":"revoked","secret":"unexpected"}`, false},
		{"empty_rotate", "code.rotate", `{}`, false},
		{"legacy_secret_shape", "code.rotate", `{"code_id":"replacement","generation":2,"type":"long_lived","scope":["info"],"expires_at":null,"secret":"new-secret"}`, false},
		{"wrong_previous", "code.rotate", strings.Replace(rotationReplyFixture, `"original"`, `"wrong"`, 1), false},
		{"wrong_replacement", "code.rotate", strings.Replace(rotationReplyFixture, `"replacement"`, `"wrong"`, 1), false},
		{"wrong_intent", "code.rotate", strings.Replace(rotationReplyFixture, "393a63", "000000", 1), false},
		{"wrong_uid", "code.rotate", strings.Replace(rotationReplyFixture, strings.Repeat("b", 32), strings.Repeat("d", 32), 1), false},
		{"wrong_ownership", "code.rotate", strings.Replace(rotationReplyFixture, strings.Repeat("c", 32), strings.Repeat("d", 32), 1), false},
		{"zero_generation", "code.rotate", strings.Replace(rotationReplyFixture, `"generation":2`, `"generation":0`, 1), false},
		{"overflow_generation", "code.rotate", strings.Replace(rotationReplyFixture, `"generation":2`, `"generation":9223372036854775808`, 1), false},
		{"unknown_type", "code.rotate", strings.Replace(rotationReplyFixture, `"long_lived"`, `"future"`, 1), false},
		{"unknown_scope", "code.rotate", strings.Replace(rotationReplyFixture, `["info"]`, `["admin"]`, 1), false},
		{"duplicate_scope", "code.rotate", strings.Replace(rotationReplyFixture, `["info"]`, `["info","info"]`, 1), false},
		{"missing_expiry", "code.rotate", strings.Replace(rotationReplyFixture, `"long_lived"`, `"expiring"`, 1), false},
		{"missing_policy", "code.rotate", strings.Replace(rotationReplyFixture, `"require_recipient_approval":true,`, ``, 1), false},
		{"null_policy", "code.rotate", strings.Replace(rotationReplyFixture, `"require_recipient_approval":true`, `"require_recipient_approval":null`, 1), false},
		{"unknown_state", "code.rotate", strings.Replace(rotationReplyFixture, `"active"`, `"future"`, 1), false},
		{"secret_in_reply", "code.rotate", strings.TrimSuffix(rotationReplyFixture, "}") + `,"secret":"new-secret"}`, false},
		{"trailing_json", "code.rotate", rotationReplyFixture + `{}`, false},
		{"missing_context", "code.revoke", `{"code_id":"original","state":"revoked"}`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newSettingsWire(t)
			if err := f.d.store.SetAccount("xr_account_" + strings.Repeat("a", 64)); err != nil {
				t.Fatal(err)
			}
			connected := make(chan error, 1)
			go func() { connected <- f.d.connectDevice() }()
			t.Cleanup(func() {
				f.d.Stop()
				select {
				case <-connected:
				case <-time.After(time.Second):
					t.Error("device reader did not stop")
				}
			})
			f.next(t, protocol.TypeHelloDevice)
			f.replies <- protocol.Message{Type: protocol.TypeHelloOK, Role: protocol.RoleDevice}
			f.ack(f.next(t, protocol.TypeDeviceSettings))
			for {
				f.d.mu.Lock()
				ready := f.d.device != nil
				f.d.mu.Unlock()
				if ready {
					break
				}
				select {
				case <-f.d.ctx.Done():
					t.Fatal("device not ready")
				case <-time.After(time.Millisecond):
				}
			}
			req := access.Request{Action: tc.action, CodeID: "original"}
			if tc.action == "code.rotate" {
				req.NewCodeID, req.NewSecret = "replacement", "new-secret"
			}
			done := make(chan ipc.Frame, 1)
			go func() { done <- f.d.manageDeviceAccess("result", req) }()
			message := f.next(t, protocol.TypeAccessRequest)
			ack := protocol.Message{Type: protocol.TypeAccessResult, RequestID: message.RequestID, Payload: json.RawMessage(tc.payload)}
			if !tc.missingContext {
				ack.DeviceContext = &invocation.DeviceContext{Authority: strings.Repeat("a", 32), DeviceUID: strings.Repeat("b", 32), OwnershipID: strings.Repeat("c", 32)}
			}
			f.replies <- ack
			select {
			case result := <-done:
				if result.OK == nil || *result.OK || result.Code != protocol.RelayError || len(result.Result) != 0 {
					t.Fatalf("malformed acknowledgement reported success: code=%s", result.Code)
				}
			case <-f.d.ctx.Done():
				t.Fatal("management result missing")
			}
		})
	}
}

// Metadata is a transaction snapshot, not a fresh execution authorization.
// Historical expiry and a false approval flag are preserved, never normalized.
func TestDeviceRotationReceipt_shouldPreservePastExpiry_andPrivateIntentBoundary(t *testing.T) {
	req := access.Request{Action: "code.rotate", CodeID: "original", NewCodeID: "replacement", NewSecret: "new-secret"}
	device := &invocation.DeviceContext{Authority: strings.Repeat("a", 32), DeviceUID: strings.Repeat("b", 32), OwnershipID: strings.Repeat("c", 32)}
	for _, raw := range []string{rotationReplyFixture,
		strings.Replace(strings.Replace(rotationReplyFixture, `"long_lived"`, `"expiring","expires_at":"2026-01-02T00:00:00Z"`, 1), `"require_recipient_approval":true`, `"require_recipient_approval":false`, 1)} {
		got, err := decodeDeviceAccessResult(req, json.RawMessage(raw), device)
		if err != nil {
			t.Fatal("valid historical metadata refused", err)
		}
		result := got.(access.CodeRotationResult)
		if result.Code.ID != "replacement" || result.Code.IssuedAt.Format(time.RFC3339) != "2026-01-01T00:00:00Z" {
			t.Fatal("metadata changed")
		}
		encoded, _ := json.Marshal(got)
		if strings.Contains(string(encoded), "intent_sha256") || strings.Contains(string(encoded), req.NewSecret) {
			t.Fatal("private input reached head")
		}
		if result.Code.ExpiresAt != nil && (result.Code.RequireApproval || result.Code.ExpiresAt.Format(time.RFC3339) != "2026-01-02T00:00:00Z") {
			t.Fatal("historical expiry/policy changed")
		}
	}
	req.NewSecret = "different-secret"
	if _, err := decodeDeviceAccessResult(req, json.RawMessage(rotationReplyFixture), device); err == nil {
		t.Fatal("different input matched the receipt")
	}
}
