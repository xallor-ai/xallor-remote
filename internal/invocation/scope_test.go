package invocation

import (
	"strings"
	"testing"
)

// Claimed devices require a complete principal; anonymous access is a bearer
// group, never a synthesized user. Missing or ambiguous identities are invalid.
func TestScope_should_validateDistinctIdentityModes(t *testing.T) {
	id := strings.Repeat("a", 32)
	bearer := Scope{Authority: id, DeviceUID: id, GrantID: id}
	owner := Scope{Authority: id, DeviceUID: id, OwnershipID: id, PrincipalID: id, TenantID: id}
	if !bearer.Valid() || !owner.Valid() {
		t.Fatal("valid scope rejected")
	}
	for _, change := range []func(*Scope){
		func(s *Scope) { s.Authority = "" }, func(s *Scope) { s.DeviceUID = "../x" },
		func(s *Scope) { s.PrincipalID = "" }, func(s *Scope) { s.TenantID = "" },
		func(s *Scope) { s.GrantID = id }, func(s *Scope) { s.OwnershipID = "" },
	} {
		s := owner
		change(&s)
		if s.Valid() {
			t.Fatal("invalid scope accepted", s)
		}
	}
}
