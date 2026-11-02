package relay

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/xallor-ai/xallor-remote/internal/access"
	"github.com/xallor-ai/xallor-remote/internal/principal"
)

func (s *SQLStore) ResolveAccess(ctx context.Context, device access.Device, actor principal.Identity) (access.Authorization, error) {
	if device.OwnershipID == "" {
		return access.Authorization{}, access.ErrDenied
	}
	if err := s.Verify(ctx, actor); err != nil {
		return access.Authorization{}, err
	}
	a := access.Authorization{Binding: device.Binding()}
	if device.OwnedBy(actor) {
		a.Permissions = access.AllPermissions()
		return a, nil
	}
	err := s.db.QueryRowContext(ctx, `SELECT id,permissions,expires_at FROM device_shares
WHERE ownership_id=? AND membership_id=? AND expires_at>?`, device.OwnershipID, actor.MembershipID, time.Now().Unix()).Scan(&a.ShareID, &a.Permissions, &a.ExpiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		return a, access.ErrDenied
	}
	return a, err
}

func (s *SQLStore) VerifyAccess(ctx context.Context, actor principal.Identity, expected access.Authorization, operation string) error {
	current, err := scanDevice(s.db.QueryRowContext(ctx, deviceFields+` WHERE d.uid=?`, expected.Binding.DeviceUID))
	if errors.Is(err, ErrDeviceNotFound) {
		return access.ErrDenied
	}
	if err != nil {
		return err
	}
	a, err := s.ResolveAccess(ctx, current, actor)
	if err != nil {
		return err
	}
	if a != expected || !a.Permissions.Allows(operation) {
		return access.ErrDenied
	}
	return nil
}

func accessState(ctx context.Context, tx *sql.Tx, device access.Device) (access.State, error) {
	state := access.State{DeviceUID: device.UID, OwnershipID: device.OwnershipID, OwnerTenant: device.OwnerTenant, OwnerPrincipal: device.OwnerPrincipal, Shares: []access.Share{}}
	rows, err := tx.QueryContext(ctx, `SELECT id,membership_id,permissions,expires_at FROM device_shares WHERE ownership_id=? ORDER BY id`, device.OwnershipID)
	if err != nil {
		return state, err
	}
	defer rows.Close()
	for rows.Next() {
		var share access.Share
		var permissions access.Permissions
		if err := rows.Scan(&share.ID, &share.MembershipID, &permissions, &share.ExpiresAt); err != nil {
			return state, err
		}
		share.Operations = permissions.Names()
		state.Shares = append(state.Shares, share)
	}
	return state, rows.Err()
}

func activeMember(ctx context.Context, tx *sql.Tx, id string) (tenant, person string, err error) {
	err = tx.QueryRowContext(ctx, `SELECT tenant_id,principal_id FROM memberships WHERE id=? AND active=1`, id).Scan(&tenant, &person)
	if errors.Is(err, sql.ErrNoRows) {
		err = access.ErrDenied
	}
	return
}
