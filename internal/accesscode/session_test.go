package accesscode

import (
	"strings"
	"testing"
	"time"
)

func sessionRequestFixture() SessionRequest {
	expires := time.Date(2026, 11, 18, 8, 0, 0, 0, time.UTC)
	return SessionRequest{RequestID: "request-1", SessionID: "session-1", TargetUID: "device-uid", CodeID: "code-1", Generation: 4, Scope: []string{"exec", "info"}, SessionExpiresAt: &expires}
}

// Scope ordering is a set property, while every authorization fact remains in the digest.
func TestSessionRequest_shouldCanonicalizeScope_andBindAllAuthorizationFacts(t *testing.T) {
	a := sessionRequestFixture()
	b := sessionRequestFixture()
	b.Scope = []string{"info", "exec", "info"}
	left, err := a.Digest()
	if err != nil {
		t.Fatal(err)
	}
	right, err := b.Digest()
	if err != nil || left != right {
		t.Fatalf("scope ordering changed digest left=%s right=%s err=%v", left, right, err)
	}
	for name, mutate := range map[string]func(*SessionRequest){
		"request":    func(v *SessionRequest) { v.RequestID = "request-2" },
		"session":    func(v *SessionRequest) { v.SessionID = "session-2" },
		"target":     func(v *SessionRequest) { v.TargetUID = "device-2" },
		"code":       func(v *SessionRequest) { v.CodeID = "code-2" },
		"generation": func(v *SessionRequest) { v.Generation++ },
		"scope":      func(v *SessionRequest) { v.Scope = []string{"info"} },
		"expiry":     func(v *SessionRequest) { next := v.SessionExpiresAt.Add(time.Minute); v.SessionExpiresAt = &next },
	} {
		t.Run(name, func(t *testing.T) {
			changed := a
			changed.Scope = append([]string(nil), a.Scope...)
			mutate(&changed)
			digest, err := changed.Digest()
			if err != nil || digest == left {
				t.Fatalf("mutation did not change digest: %s err=%v", digest, err)
			}
		})
	}
}

func TestSessionRequest_shouldRejectMissingAuthorizationFacts(t *testing.T) {
	base := sessionRequestFixture()
	for name, mutate := range map[string]func(*SessionRequest){
		"request":    func(v *SessionRequest) { v.RequestID = "" },
		"session":    func(v *SessionRequest) { v.SessionID = "" },
		"target":     func(v *SessionRequest) { v.TargetUID = "" },
		"code":       func(v *SessionRequest) { v.CodeID = "" },
		"generation": func(v *SessionRequest) { v.Generation = 0 },
		"scope":      func(v *SessionRequest) { v.Scope = nil },
	} {
		t.Run(name, func(t *testing.T) {
			value := base
			mutate(&value)
			if _, err := value.Digest(); err == nil || !strings.Contains(err.Error(), "invalid") {
				t.Fatalf("invalid request accepted: %+v err=%v", value, err)
			}
		})
	}
}
