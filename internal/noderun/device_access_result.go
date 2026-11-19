package noderun

import (
	"bytes"
	"crypto/subtle"
	"encoding/json"
	"io"
	"math"
	"unicode/utf8"

	"github.com/xallor-ai/xallor-remote/internal/access"
	"github.com/xallor-ai/xallor-remote/internal/accesscode"
	"github.com/xallor-ai/xallor-remote/internal/invocation"
	"github.com/xallor-ai/xallor-remote/internal/protocol"
)

// A management acknowledgement must match its action and requested code. An
// invalid success payload is still an unknown mutation outcome, never a retry.
func decodeDeviceAccessResult(req access.Request, raw json.RawMessage, device *invocation.DeviceContext) (any, error) {
	if len(raw) > 16*1024 || !utf8.Valid(raw) {
		return nil, access.ErrInvalid
	}
	switch req.Action {
	case "code.revoke":
		var result access.CodeRevocationResult
		if decodeCodeResult(raw, &result) != nil || result.CodeID != req.CodeID || result.State != accesscode.StateRevoked {
			return nil, access.ErrInvalid
		}
		return result, nil
	case "code.rotate":
		var result access.CodeRotationReply
		intent, err := accesscode.RotationDigest(req.CodeID, req.NewCodeID, protocol.SHA256Hex(req.NewSecret))
		if err != nil || decodeCodeResult(raw, &result) != nil || result.PreviousCodeID != req.CodeID || result.Code.ID != req.NewCodeID ||
			subtle.ConstantTimeCompare([]byte(result.IntentSHA256), []byte(intent)) != 1 || result.Code.Validate() != nil || result.Code.Generation < 2 || result.Code.Generation > math.MaxInt64 {
			return nil, access.ErrInvalid
		}
		if device == nil || !device.Valid() || device.OwnershipID == "" || result.Code.DeviceUID != device.DeviceUID || result.Code.OwnershipID != device.OwnershipID {
			return nil, access.ErrInvalid
		}
		permissions, err := access.ParsePermissions(result.Code.Scope)
		if err != nil || len(permissions.Names()) != len(result.Code.Scope) {
			return nil, access.ErrInvalid
		}
		var presence struct {
			Code struct {
				Approval *bool `json:"require_recipient_approval"`
			} `json:"code"`
		}
		if json.Unmarshal(raw, &presence) != nil || presence.Code.Approval == nil {
			return nil, access.ErrInvalid
		}
		return result.CodeRotationResult, nil
	case "claim", "status", "transfer", "share", "unshare":
		var state access.State
		if json.Unmarshal(raw, &state) != nil || state.DeviceUID == "" {
			return nil, access.ErrInvalid
		}
		return state, nil
	default:
		return nil, access.ErrInvalid
	}
}

func decodeCodeResult(raw json.RawMessage, value any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return access.ErrInvalid
	}
	return nil
}
