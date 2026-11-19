package relay

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/xallor-ai/xallor-remote/internal/access"
	"github.com/xallor-ai/xallor-remote/internal/accesscode"
	"github.com/xallor-ai/xallor-remote/internal/protocol"
)

// Reusing an authenticated rotation intent must observe its original child's
// current persisted state, never follow a later rotation or echo bearer input.
func TestCodeRotationReceipt_shouldReportOriginalChildState_withoutSecret(t *testing.T) {
	for _, change := range []string{"active", "revoked", "consumed", "rotated-again", "naturally-expired"} {
		t.Run(change, func(t *testing.T) {
			s, h, target, client, device, token := typedCodeFixture(t)
			if change == "consumed" {
				if _, err := s.faults.Exec(`UPDATE access_codes SET type='one_time' WHERE code_id='managed-code'`); err != nil {
					t.Fatal(err)
				}
			}
			var expiry int64
			if change == "naturally-expired" {
				expiry = time.Now().Add(2 * time.Second).Unix()
				if _, err := s.faults.Exec(`UPDATE access_codes SET type='expiring',issued_at=?,expires_at=? WHERE code_id='managed-code'`, time.Now().Add(-time.Minute).Unix(), expiry); err != nil {
					t.Fatal(err)
				}
			}
			req := access.Request{Action: "code.rotate", CodeID: "managed-code", NewCodeID: "receipt-child", NewSecret: "receipt-only-private-secret"}
			first := sendTypedAccess(t, h, target, token, req)
			if first.Code != "" {
				t.Fatal("first rotation failed", first.Code)
			}
			var original access.CodeRotationReply
			if json.Unmarshal(first.Payload, &original) != nil {
				t.Fatal("invalid initial metadata")
			}
			want := accesscode.StateActive
			switch change {
			case "revoked":
				if got := sendTypedAccess(t, h, target, token, access.Request{Action: "code.revoke", CodeID: req.NewCodeID}); got.Code != "" {
					t.Fatal(got.Code)
				}
				want = accesscode.StateRevoked
			case "consumed":
				expires := time.Now().Add(time.Hour).Unix()
				approveSession(t, h, target, client, protocol.Message{Type: protocol.TypeSessionRequest, RequestID: "consume-child", SessionID: "child-session", TargetID: "device", TargetUID: device.UID, CodeID: req.NewCodeID, CodeGeneration: 2, Grant: req.NewSecret, SessionScope: []string{protocol.OpInfo}, SessionExpiresAt: &expires})
				if client.last().Status != protocol.SessionActive {
					t.Fatal("child not consumed")
				}
				want = accesscode.StateConsumed
			case "rotated-again":
				if got := sendTypedAccess(t, h, target, token, access.Request{Action: "code.rotate", CodeID: req.NewCodeID, NewCodeID: "grandchild", NewSecret: "other-private-secret"}); got.Code != "" {
					t.Fatal(got.Code)
				}
				want = accesscode.StateRevoked
			case "naturally-expired":
				<-time.After(time.Until(time.Unix(expiry, 0)))
			}
			got := sendTypedAccess(t, h, target, token, req)
			if got.Code != "" {
				t.Fatal("matching receipt not returned", got.Code)
			}
			var result struct {
				PreviousCodeID string          `json:"previous_code_id"`
				Code           accesscode.Code `json:"code"`
				IntentSHA256   string          `json:"intent_sha256"`
			}
			if err := json.Unmarshal(got.Payload, &result); err != nil {
				t.Fatal(err)
			}
			if result.PreviousCodeID != "managed-code" || result.Code.ID != "receipt-child" || result.Code.Generation != 2 || result.Code.State != want || !result.Code.RequireApproval {
				t.Error("receipt lost original child identity/state/policy")
			}
			if result.Code.DeviceUID != device.UID || result.Code.OwnershipID != device.OwnershipID {
				t.Error("receipt lost ownership binding")
			}
			if bytes.Contains(got.Payload, []byte(req.NewSecret)) {
				t.Error("receipt exposed plaintext input")
			}
			var receipt accesscode.Rotation
			if err := s.ReadAccessCode(target.Context(), func(r accesscode.Reader) error {
				var err error
				receipt, err = r.Rotation(target.Context(), req.CodeID)
				return err
			}); err != nil {
				t.Fatal(err)
			}
			if result.IntentSHA256 != receipt.IntentSHA256 {
				t.Error("wire reply is not bound to committed intent")
			}
			if !result.Code.IssuedAt.Equal(original.Code.IssuedAt) {
				t.Error("receipt retry changed issuance time")
			}
			if expiry != 0 && (result.Code.ExpiresAt == nil || result.Code.ExpiresAt.Unix() != expiry || result.Code.ExpiresAt.After(time.Now())) {
				t.Error("natural expiry was extended or not exercised")
			}
		})
	}
}

// A metadata value computed inside the callback cannot escape as success if
// commit's acknowledgement is unknown, even when the DB really committed.
func TestCodeRotationReceipt_shouldNotPublishMetadata_whenCommitAcknowledgementIsLost(t *testing.T) {
	s, h, target, _, _, token := typedCodeFixture(t)
	h.accessCodes = uncertainCodeManagementStore{Store: s.SQLStore}
	got := sendTypedAccess(t, h, target, token, managementAuthorityRequest("code.rotate"))
	if got.Code != protocol.RelayError || len(got.Payload) != 0 || h.DeviceOnline("device") {
		t.Fatal("unknown commit published successful metadata")
	}
	if err := s.ReadAccessCode(context.Background(), func(r accesscode.Reader) error {
		receipt, err := r.Rotation(context.Background(), "managed-code")
		if err == nil && receipt.ReplacementCodeID != "new-managed-code" {
			t.Error("wrong committed receipt")
		}
		return err
	}); err != nil {
		t.Fatal("test did not actually commit rotation", err)
	}
}

// Return the authorized transaction's snapshot, not an unguarded second read
// mixed with a different writer's post-commit changes. A later request observes
// those changes through a fresh authorized transaction.
func TestCodeRotationReceipt_shouldPublishOneSnapshot_whenChildChangesAfterCommit(t *testing.T) {
	s, h, target, _, _, token := typedCodeFixture(t)
	ctx := context.Background()
	h.accessCodes = afterCommittedCodeStore{Store: s.SQLStore, after: func() error {
		return s.WriteAccessCode(ctx, func(tx accesscode.Transaction) error { _, err := tx.RevokeCode(ctx, "new-managed-code"); return err })
	}}
	req := managementAuthorityRequest("code.rotate")
	for _, want := range []accesscode.CodeState{accesscode.StateActive, accesscode.StateRevoked} {
		got := sendTypedAccess(t, h, target, token, req)
		var result access.CodeRotationReply
		if got.Code != "" || json.Unmarshal(got.Payload, &result) != nil || result.Code.State != want {
			t.Fatal("transaction snapshot was mixed with later state", got.Code)
		}
	}
}
