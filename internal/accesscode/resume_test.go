package accesscode

import (
	"errors"
	"strings"
	"testing"
	"time"
)

var resumeNow = time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC)

func resumeFixture() ResumeCapability {
	return ResumeCapability{
		ID: "resume-1", SessionID: "session-1", BindingKind: "device",
		BindingDigest: strings.Repeat("b", 64), CapabilityHash: strings.Repeat("a", 64),
		IssuedAt: resumeNow, ExpiresAt: resumeNow.Add(time.Minute), Generation: 1, State: ResumeActive,
	}
}

func TestResume_shouldConsumeOnlyOnce_andRejectChangedBinding(t *testing.T) {
	resume := resumeFixture()
	if err := resume.CanUse(resumeNow.Add(time.Second), "session-1", "device", strings.Repeat("b", 64)); err != nil {
		t.Fatal(err)
	}
	if err := resume.CanUse(resumeNow.Add(time.Second), "session-1", "device", strings.Repeat("c", 64)); !errors.Is(err, ErrResumeBinding) {
		t.Fatalf("changed binding accepted: %v", err)
	}
	consumed, err := resume.Consume(resumeNow.Add(time.Second))
	if err != nil || consumed.State != ResumeConsumed {
		t.Fatalf("consume=%+v err=%v", consumed, err)
	}
	if err := consumed.CanUse(resumeNow.Add(2*time.Second), consumed.SessionID, consumed.BindingKind, consumed.BindingDigest); !errors.Is(err, ErrResumeConsumed) {
		t.Fatalf("consumed capability reusable: %v", err)
	}
}

func TestResume_shouldRejectExpiry_andPreserveUnknownWithoutReplay(t *testing.T) {
	resume := resumeFixture()
	if err := resume.CanUse(resume.ExpiresAt, resume.SessionID, resume.BindingKind, resume.BindingDigest); !errors.Is(err, ErrResumeExpired) {
		t.Fatalf("expiry boundary accepted: %v", err)
	}
	unknown, err := resume.MarkUnknown(resumeNow.Add(time.Second))
	if err != nil || unknown.State != ResumeUnknown {
		t.Fatalf("unknown=%+v err=%v", unknown, err)
	}
	if err := unknown.CanUse(resumeNow.Add(2*time.Second), unknown.SessionID, unknown.BindingKind, unknown.BindingDigest); !errors.Is(err, ErrResumeState) {
		t.Fatalf("unknown capability replayed: %v", err)
	}
}

func TestResume_shouldRevokeOnlyActiveCapability(t *testing.T) {
	resume := resumeFixture()
	revoked, err := resume.Revoke(resumeNow.Add(time.Second))
	if err != nil || revoked.State != ResumeRevoked {
		t.Fatalf("revoke=%+v err=%v", revoked, err)
	}
	if _, err := revoked.Revoke(resumeNow.Add(2 * time.Second)); !errors.Is(err, ErrResumeState) {
		t.Fatalf("revoked capability revoked again: %v", err)
	}
}
