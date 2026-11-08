// Package entitlement evaluates provider-supplied commercial values. It does
// not own billing, usage persistence, transport limits, or the local job
// ledger; callers must reserve those resources with their own lifecycle.
package entitlement

import (
	"context"
	"errors"
	"time"
)

const (
	ExecConcurrent       = "exec.concurrent"
	TerminalConcurrent   = "terminal.concurrent"
	RelayBytesPerSecond  = "relay.bytes_per_second"
	RelayMonthlyEgress   = "relay.monthly_egress_bytes"
	FileMaxTransferBytes = "file.max_transfer_bytes"
)

var ErrProviderUnavailable = errors.New("entitlement provider unavailable")

type Snapshot struct {
	TenantID          string
	PrincipalID       string
	MembershipID      string
	MembershipVersion int64
	MembershipActive  bool
	ExpiresAt         int64
	Values            map[string]int64
}

type Request struct {
	TenantID          string
	PrincipalID       string
	MembershipID      string
	MembershipVersion int64
	Feature           string
	Amount            int64
}

type Decision struct {
	Allowed bool
	Code    string
	Feature string
	Limit   int64
}

type Provider interface {
	Snapshot(context.Context, string, string, string) (Snapshot, error)
}

type Service struct {
	Provider Provider
	Now      func() time.Time
}

func (s Service) Evaluate(ctx context.Context, req Request) Decision {
	if s.Provider == nil {
		return Decision{Code: "entitlement_unavailable", Feature: req.Feature}
	}
	snapshot, err := s.Provider.Snapshot(ctx, req.TenantID, req.PrincipalID, req.MembershipID)
	if err != nil {
		return Decision{Code: "entitlement_unavailable", Feature: req.Feature}
	}
	now := time.Now
	if s.Now != nil {
		now = s.Now
	}
	return Decide(snapshot, req, now())
}

func Decide(snapshot Snapshot, req Request, now time.Time) Decision {
	d := Decision{Feature: req.Feature}
	if req.TenantID == "" || req.PrincipalID == "" || req.MembershipID == "" || req.MembershipVersion <= 0 || req.Amount <= 0 || snapshot.TenantID != req.TenantID || snapshot.PrincipalID != req.PrincipalID || snapshot.MembershipID != req.MembershipID || snapshot.MembershipVersion != req.MembershipVersion {
		d.Code = "unauthorized"
		return d
	}
	if !snapshot.MembershipActive {
		d.Code = "entitlement_inactive"
		return d
	}
	if snapshot.ExpiresAt <= 0 || now.Unix() >= snapshot.ExpiresAt {
		d.Code = "entitlement_expired"
		return d
	}
	limit, ok := snapshot.Values[req.Feature]
	if !ok || limit <= 0 {
		d.Code = "entitlement_missing"
		return d
	}
	d.Limit = limit
	if req.Amount > limit {
		d.Code = "entitlement_exceeded"
		return d
	}
	d.Allowed = true
	return d
}
