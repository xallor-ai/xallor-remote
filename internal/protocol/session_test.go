package protocol

import (
	"strings"
	"testing"
	"time"
)

func sessionMessageFixture() Message {
	expires := time.Date(2026, 11, 18, 8, 0, 0, 0, time.UTC).Unix()
	return Message{Type: TypeSessionRequest, RequestID: "request-1", SessionID: "session-1", TargetID: "device-1", TargetUID: "uid-1", CodeID: "code-1", CodeGeneration: 2, SessionScope: []string{OpExec, OpInfo}, SessionExpiresAt: &expires}
}

// Structured session requests convert to the shared accesscode digest input.
func TestSessionRequest_shouldBindScopeGenerationAndExpiry(t *testing.T) {
	first := sessionMessageFixture()
	request, err := first.AccessCodeSessionRequest()
	if err != nil {
		t.Fatal(err)
	}
	left, err := request.Digest()
	if err != nil {
		t.Fatal(err)
	}
	second := first
	second.SessionScope = []string{OpInfo, OpExec, OpInfo}
	rightRequest, err := second.AccessCodeSessionRequest()
	if err != nil {
		t.Fatal(err)
	}
	right, err := rightRequest.Digest()
	if err != nil || left != right {
		t.Fatalf("scope order changed digest left=%s right=%s err=%v", left, right, err)
	}
	second.CodeGeneration++
	changed, err := second.AccessCodeSessionRequest()
	if err != nil {
		t.Fatal(err)
	}
	changedDigest, err := changed.Digest()
	if err != nil || changedDigest == left {
		t.Fatalf("generation mutation kept digest %s", changedDigest)
	}
}

func TestSessionApproval_shouldRejectMissingDigestAndNeverCarryCodeSecret(t *testing.T) {
	request := sessionMessageFixture()
	request.Grant = "xr_grant_secret"
	if _, err := request.AccessCodeSessionRequest(); err != nil {
		t.Fatal(err)
	}
	approval := Message{Type: TypeSessionApprovalResult, RequestID: request.RequestID, SessionID: request.SessionID, RequestSHA256: ""}
	if err := ValidateSessionApprovalResult(approval); err == nil || !strings.Contains(err.Error(), "digest") {
		t.Fatalf("missing approval digest accepted: %v", err)
	}
	encoded, err := (Message{Type: TypeSessionApprovalRequest, RequestID: request.RequestID, SessionID: request.SessionID, CodeID: request.CodeID, CodeGeneration: request.CodeGeneration, SessionScope: request.SessionScope, RequestSHA256: "digest"}).Bytes()
	if err != nil || strings.Contains(string(encoded), "xr_grant_secret") {
		t.Fatalf("approval envelope leaked secret: %s err=%v", encoded, err)
	}
}

func TestSessionResult_shouldRequireTerminalStateAndSessionIdentity(t *testing.T) {
	valid := Message{Type: TypeSessionResult, RequestID: "request-1", SessionID: "session-1", Status: SessionActive, RequestSHA256: strings.Repeat("a", 64)}
	if err := ValidateSessionResult(valid); err != nil {
		t.Fatal(err)
	}
	for _, status := range []string{"", "bogus"} {
		invalid := valid
		invalid.Status = status
		if err := ValidateSessionResult(invalid); err == nil {
			t.Fatalf("status %q accepted", status)
		}
	}
}

func TestSessionResume_shouldRequireSeparateSingleUseCapability(t *testing.T) {
	valid := Message{Type: TypeSessionResumeRequest, RequestID: "request-1", SessionID: "session-1", ResumeID: "resume-1", ResumeCapability: "opaque-once", TargetUID: "target-1"}
	if err := ValidateSessionResumeRequest(valid); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"ResumeID", "ResumeCapability", "TargetUID"} {
		invalid := valid
		switch field {
		case "ResumeID":
			invalid.ResumeID = ""
		case "ResumeCapability":
			invalid.ResumeCapability = ""
		case "TargetUID":
			invalid.TargetUID = ""
		}
		if err := ValidateSessionResumeRequest(invalid); err == nil {
			t.Fatalf("missing %s accepted", field)
		}
	}
	result := Message{Type: TypeSessionResumeResult, RequestID: valid.RequestID, SessionID: valid.SessionID, Status: SessionActive}
	if err := ValidateSessionResumeResult(result); err != nil {
		t.Fatal(err)
	}
}
