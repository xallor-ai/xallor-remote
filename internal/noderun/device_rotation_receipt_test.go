package noderun

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

// TLS Relay/SQLite/Runtime IPC must preserve current receipt metadata while
// stripping both the input secret and private intent verifier from head output.
func TestDeviceRotationReceipt_shouldReturnRevokedChild_withoutSecretOrDigest(t *testing.T) {
	d, s, device := newDeviceCodeRuntime(t)
	ctx := context.Background()
	code := accesscode.Code{ID: "receipt-source", DeviceUID: device.UID, OwnershipID: device.OwnershipID, Type: accesscode.LongLived, Generation: 1, IssuedAt: time.Now().UTC(), Scope: []string{protocol.OpInfo}, RequireApproval: true, State: accesscode.StateActive}
	if err := s.WriteAccessCode(ctx, func(tx accesscode.Transaction) error {
		return tx.InsertCode(ctx, code, protocol.SHA256Hex("old-receipt-secret"))
	}); err != nil {
		t.Fatal(err)
	}
	req := access.Request{Action: "code.rotate", CodeID: code.ID, NewCodeID: "receipt-replacement", NewSecret: "private-new-rotation-secret"}
	first := deviceAccessIPC(t, d, req)
	if first.OK == nil || !*first.OK {
		t.Fatal("initial rotation failed", first.Code)
	}
	if bytes.Contains(first.Result, []byte(req.NewSecret)) || bytes.Contains(first.Result, []byte("intent_sha256")) {
		t.Error("private material exposed to local head")
	}
	if got := deviceAccessIPC(t, d, access.Request{Action: "code.revoke", CodeID: req.NewCodeID}); got.OK == nil || !*got.OK {
		t.Fatal("child revoke failed", got.Code)
	}
	repeated := deviceAccessIPC(t, d, req)
	if repeated.OK == nil || !*repeated.OK {
		t.Fatal("receipt retry failed", repeated.Code)
	}
	var result struct {
		PreviousCodeID string          `json:"previous_code_id"`
		Code           accesscode.Code `json:"code"`
	}
	if err := json.Unmarshal(repeated.Result, &result); err != nil {
		t.Fatal(err)
	}
	if result.PreviousCodeID != code.ID || result.Code.ID != req.NewCodeID || result.Code.Generation != 2 || result.Code.State != accesscode.StateRevoked {
		t.Error("Runtime hid revoked replacement state")
	}
	if bytes.Contains(repeated.Result, []byte(req.NewSecret)) || bytes.Contains(repeated.Result, []byte("intent_sha256")) {
		t.Error("receipt retry leaked private material")
	}
}
