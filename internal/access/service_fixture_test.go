package access

import (
	"context"
	"errors"
	"time"

	"github.com/xallor-ai/xallor-remote/internal/principal"
)

// The fake implements persistence only; all ownership, permission, expiry and
// identity decisions under test must come from Service.
type accessData struct {
	device  Device
	actor   principal.Identity
	members map[string]Member
	shares  map[string]ShareRecord
}

type accessMemory struct {
	data      accessData
	writes    int
	commitErr error
}

func (m *accessMemory) ReadAccess(ctx context.Context, fn func(Reader) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return fn(&m.data)
}
func (m *accessMemory) WriteAccess(ctx context.Context, fn func(Transaction) error) error {
	m.writes++
	if err := ctx.Err(); err != nil {
		return err
	}
	copy := m.data
	copy.shares = map[string]ShareRecord{}
	for id, row := range m.data.shares {
		copy.shares[id] = row
	}
	if err := fn(&copy); err != nil {
		return err
	}
	m.data = copy
	return m.commitErr
}
func (d *accessData) Principal(_ context.Context, id string, now int64) (principal.Identity, error) {
	if id != d.actor.CredentialID || d.actor.ExpiresAt <= now {
		return principal.Identity{}, principal.ErrUnauthorized
	}
	return d.actor, nil
}
func (d *accessData) DeviceByID(context.Context, string) (Device, error) { return d.device, nil }
func (d *accessData) DeviceByUID(_ context.Context, uid string) (Device, error) {
	if uid != d.device.UID {
		return Device{}, ErrDenied
	}
	return d.device, nil
}
func (d *accessData) Member(_ context.Context, id string) (Member, error) {
	r, ok := d.members[id]
	if !ok {
		return Member{}, ErrDenied
	}
	return r, nil
}
func (d *accessData) Share(_ context.Context, ownership, member string) (ShareRecord, error) {
	for _, r := range d.shares {
		if r.OwnershipID == ownership && r.MembershipID == member {
			return r, nil
		}
	}
	return ShareRecord{}, ErrDenied
}
func (d *accessData) Shares(_ context.Context, ownership string) ([]ShareRecord, error) {
	var rows []ShareRecord
	for _, r := range d.shares {
		if r.OwnershipID == ownership {
			rows = append(rows, r)
		}
	}
	return rows, nil
}
func (d *accessData) ReplaceOwnership(_ context.Context, r Ownership) error {
	d.device.OwnershipID, d.device.OwnerTenant, d.device.OwnerPrincipal = r.ID, r.TenantID, r.PrincipalID
	d.shares = map[string]ShareRecord{}
	return nil
}
func (d *accessData) DeleteMemberShare(_ context.Context, ownership, member string) error {
	for id, r := range d.shares {
		if r.OwnershipID == ownership && r.MembershipID == member {
			delete(d.shares, id)
		}
	}
	return nil
}
func (d *accessData) DeleteExpiredShares(_ context.Context, ownership string, now int64) error {
	for id, r := range d.shares {
		if r.OwnershipID == ownership && r.ExpiresAt <= now {
			delete(d.shares, id)
		}
	}
	return nil
}
func (d *accessData) CountShares(ctx context.Context, ownership string) (int, error) {
	rows, err := d.Shares(ctx, ownership)
	return len(rows), err
}
func (d *accessData) InsertShare(_ context.Context, r ShareRecord) error {
	d.shares[r.ID] = r
	return nil
}
func (d *accessData) DeleteShare(_ context.Context, ownership, id string) error {
	r, ok := d.shares[id]
	if !ok || r.OwnershipID != ownership {
		return ErrDenied
	}
	delete(d.shares, id)
	return nil
}

func serviceFixture() (*Service, *accessMemory, principal.Identity) {
	actor := principal.Identity{Authority: "authority", PrincipalID: "owner", TenantID: "space", MembershipID: "owner-member", CredentialID: "token-id", MembershipVersion: 1, ExpiresAt: 2000}
	m := &accessMemory{data: accessData{
		device:  Device{UID: "device-uid", SecretHash: "device-proof", OwnershipID: "ownership", OwnerTenant: actor.TenantID, OwnerPrincipal: actor.PrincipalID},
		actor:   actor,
		members: map[string]Member{"guest": {TenantID: "other-space", PrincipalID: "other-user", Active: true}},
		shares:  map[string]ShareRecord{},
	}}
	s := NewService(m)
	s.now = func() time.Time { return time.Unix(1000, 0) }
	return s, m, actor
}

var lostCommit = errors.New("fixture: commit outcome unavailable")
