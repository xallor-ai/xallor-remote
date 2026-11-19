package access

import "github.com/xallor-ai/xallor-remote/internal/accesscode"

// CodeRevocationResult acknowledges the named mutation, not device ownership.
type CodeRevocationResult struct {
	CodeID string               `json:"code_id"`
	State  accesscode.CodeState `json:"state"`
}

// CodeRotationResult is public metadata from the committed transaction's
// original child. It is neither a bearer credential nor session authorization.
type CodeRotationResult struct {
	PreviousCodeID string          `json:"previous_code_id"`
	Code           accesscode.Code `json:"code"`
}

// CodeRotationReply binds the Relay acknowledgement to private request input.
// Runtime verifies this digest then strips it before returning the public result.
type CodeRotationReply struct {
	CodeRotationResult
	IntentSHA256 string `json:"intent_sha256"`
}
