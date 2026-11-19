package protocol

import (
	"errors"
	"regexp"
)

var sessionDigestPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)

func ValidateSessionApprovalRequest(m Message) error {
	if m.Type != TypeSessionApprovalRequest || m.RequestID == "" || m.SessionID == "" || m.CodeID == "" || m.CodeGeneration == 0 || len(m.SessionScope) == 0 || !sessionDigestPattern.MatchString(m.RequestSHA256) {
		return errors.New("invalid session approval request")
	}
	return nil
}

func ValidateSessionApprovalResult(m Message) error {
	if m.Type != TypeSessionApprovalResult || m.RequestID == "" || m.SessionID == "" || !sessionDigestPattern.MatchString(m.RequestSHA256) || m.Allow == nil {
		return errors.New("invalid session approval result: digest or decision missing")
	}
	return nil
}

func ValidateSessionResult(m Message) error {
	if m.Type != TypeSessionResult || m.RequestID == "" || m.SessionID == "" || !sessionDigestPattern.MatchString(m.RequestSHA256) {
		return errors.New("invalid session result")
	}
	switch m.Status {
	case SessionActive, SessionRejectedState, SessionExpiredState, SessionUnknownState:
		return nil
	default:
		return errors.New("invalid session result state")
	}
}

func ValidateSessionResumeRequest(m Message) error {
	if m.Type != TypeSessionResumeRequest || m.RequestID == "" || m.SessionID == "" || m.ResumeID == "" || m.ResumeCapability == "" || m.TargetUID == "" {
		return errors.New("invalid session resume request")
	}
	return nil
}

func ValidateSessionResumeResult(m Message) error {
	if m.Type != TypeSessionResumeResult || m.RequestID == "" || m.SessionID == "" {
		return errors.New("invalid session resume result")
	}
	switch m.Status {
	case SessionActive, SessionRejectedState, SessionExpiredState, SessionUnknownState:
		return nil
	default:
		return errors.New("invalid session resume result state")
	}
}
