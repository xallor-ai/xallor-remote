package entitlement

import (
	"context"
	"errors"
	"testing"
	"time"
)

type provider struct {
	snapshot Snapshot
	err      error
}

func (p provider) Snapshot(context.Context, string, string, string) (Snapshot, error) {
	return p.snapshot, p.err
}

// Entitlement admission is fail-closed and scoped to the exact tenant,
// principal, membership generation, feature value, and expiry.
func TestDecide_shouldFailClosedOnScopeExpiryAndValue(t *testing.T) {
	now := time.Unix(1000, 0)
	s := Snapshot{TenantID: "tenant", PrincipalID: "principal", MembershipID: "member", MembershipVersion: 2, MembershipActive: true, ExpiresAt: 1100, Values: map[string]int64{ExecConcurrent: 2}}
	valid := Decide(s, Request{TenantID: "tenant", PrincipalID: "principal", MembershipID: "member", MembershipVersion: 2, Feature: ExecConcurrent, Amount: 2}, now)
	if !valid.Allowed || valid.Limit != 2 {
		t.Fatalf("valid decision: %+v", valid)
	}
	for name, req := range map[string]Request{
		"wrong tenant":    {TenantID: "other", PrincipalID: "principal", MembershipID: "member", MembershipVersion: 2, Feature: ExecConcurrent, Amount: 1},
		"wrong version":   {TenantID: "tenant", PrincipalID: "principal", MembershipID: "member", MembershipVersion: 1, Feature: ExecConcurrent, Amount: 1},
		"too much":        {TenantID: "tenant", PrincipalID: "principal", MembershipID: "member", MembershipVersion: 2, Feature: ExecConcurrent, Amount: 3},
		"missing feature": {TenantID: "tenant", PrincipalID: "principal", MembershipID: "member", MembershipVersion: 2, Feature: TerminalConcurrent, Amount: 1},
	} {
		t.Run(name, func(t *testing.T) {
			got := Decide(s, req, now)
			if got.Allowed {
				t.Fatalf("unexpected allow: %+v", got)
			}
		})
	}
	expired := s
	expired.ExpiresAt = 1000
	if got := Decide(expired, Request{TenantID: "tenant", PrincipalID: "principal", MembershipID: "member", MembershipVersion: 2, Feature: ExecConcurrent, Amount: 1}, now); got.Code != "entitlement_expired" {
		t.Fatal(got)
	}
	inactive := s
	inactive.MembershipActive = false
	if got := Decide(inactive, Request{TenantID: "tenant", PrincipalID: "principal", MembershipID: "member", MembershipVersion: 2, Feature: ExecConcurrent, Amount: 1}, now); got.Code != "entitlement_inactive" {
		t.Fatal(got)
	}
}

func TestService_shouldRejectProviderFailure_withoutFallback(t *testing.T) {
	s := Service{Provider: provider{err: errors.New("provider down")}}
	got := s.Evaluate(context.Background(), Request{TenantID: "t", PrincipalID: "p", MembershipID: "m", MembershipVersion: 1, Feature: ExecConcurrent, Amount: 1})
	if got.Allowed || got.Code != "entitlement_unavailable" {
		t.Fatal(got)
	}
	if got := (Service{}).Evaluate(context.Background(), Request{Feature: ExecConcurrent, Amount: 1}); got.Code != "entitlement_unavailable" {
		t.Fatal(got)
	}
}
