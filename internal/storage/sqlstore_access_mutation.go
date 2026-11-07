package storage

import (
	"context"

	"github.com/xallor-ai/xallor-remote/internal/access"
)

func (t accessTransaction) ReplaceOwnership(ctx context.Context, r access.Ownership) error {
	// FK cascade removes shares of the old ownership in this same transaction.
	if _, err := t.tx.ExecContext(ctx, `DELETE FROM device_ownership WHERE device_uid=?`, r.DeviceUID); err != nil {
		return err
	}
	_, err := t.tx.ExecContext(ctx, `INSERT INTO device_ownership(device_uid,id,tenant_id,principal_id) VALUES (?,?,?,?)`, r.DeviceUID, r.ID, r.TenantID, r.PrincipalID)
	return err
}

func (t accessTransaction) DeleteMemberShare(ctx context.Context, ownership, member string) error {
	_, err := t.tx.ExecContext(ctx, `DELETE FROM device_shares WHERE ownership_id=? AND membership_id=?`, ownership, member)
	return err
}

func (t accessTransaction) DeleteExpiredShares(ctx context.Context, ownership string, now int64) error {
	_, err := t.tx.ExecContext(ctx, `DELETE FROM device_shares WHERE ownership_id=? AND expires_at<=?`, ownership, now)
	return err
}

func (t accessTransaction) CountShares(ctx context.Context, ownership string) (int, error) {
	var count int
	err := t.tx.QueryRowContext(ctx, `SELECT count(*) FROM device_shares WHERE ownership_id=?`, ownership).Scan(&count)
	return count, err
}

func (t accessTransaction) InsertShare(ctx context.Context, r access.ShareRecord) error {
	_, err := t.tx.ExecContext(ctx, `INSERT INTO device_shares(id,ownership_id,membership_id,permissions,expires_at) VALUES (?,?,?,?,?)`, r.ID, r.OwnershipID, r.MembershipID, r.Permissions, r.ExpiresAt)
	return err
}

func (t accessTransaction) DeleteShare(ctx context.Context, ownership, id string) error {
	result, err := t.tx.ExecContext(ctx, `DELETE FROM device_shares WHERE id=? AND ownership_id=?`, id, ownership)
	return requireDeviceMutation(result, err)
}
