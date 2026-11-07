package storage

import (
	"context"
	"database/sql"
	"errors"

	"github.com/xallor-ai/xallor-remote/internal/access"
	"github.com/xallor-ai/xallor-remote/internal/principal"
)

var _ access.Store = (*SQLStore)(nil)

type accessTransaction struct{ tx *sql.Tx }

func (s *SQLStore) ReadAccess(ctx context.Context, operation func(access.Reader) error) error {
	_, err := storeTransaction(ctx, s.db, func(tx *sql.Tx) (struct{}, error) {
		return struct{}{}, operation(accessTransaction{tx})
	})
	return err
}

func (s *SQLStore) WriteAccess(ctx context.Context, operation func(access.Transaction) error) error {
	_, err := storeTransaction(ctx, s.db, func(tx *sql.Tx) (struct{}, error) {
		return struct{}{}, operation(accessTransaction{tx})
	})
	return err
}

func (t accessTransaction) CredentialByID(ctx context.Context, id string) (principal.Credential, error) {
	return scanPrincipal(t.tx.QueryRowContext(ctx, principalQuery+`c.id=?`, id))
}

func (t accessTransaction) DeviceByID(ctx context.Context, id string) (access.Device, error) {
	return scanDevice(t.tx.QueryRowContext(ctx, selectDevice, id))
}

func (t accessTransaction) DeviceByUID(ctx context.Context, uid string) (access.Device, error) {
	device, err := scanDevice(t.tx.QueryRowContext(ctx, deviceFields+` WHERE d.uid=?`, uid))
	if errors.Is(err, access.ErrDeviceNotFound) {
		return access.Device{}, access.ErrDenied
	}
	return device, err
}

func (t accessTransaction) Member(ctx context.Context, id string) (access.Member, error) {
	var member access.Member
	err := t.tx.QueryRowContext(ctx, `SELECT tenant_id,principal_id,active FROM memberships WHERE id=?`, id).Scan(&member.TenantID, &member.PrincipalID, &member.Active)
	if errors.Is(err, sql.ErrNoRows) {
		return access.Member{}, access.ErrDenied
	}
	return member, err
}

func (t accessTransaction) Share(ctx context.Context, ownership, member string) (access.ShareRecord, error) {
	var share access.ShareRecord
	err := t.tx.QueryRowContext(ctx, `SELECT id,ownership_id,membership_id,permissions,expires_at FROM device_shares WHERE ownership_id=? AND membership_id=?`, ownership, member).Scan(&share.ID, &share.OwnershipID, &share.MembershipID, &share.Permissions, &share.ExpiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		return access.ShareRecord{}, access.ErrDenied
	}
	return share, err
}

func (t accessTransaction) Shares(ctx context.Context, ownership string) ([]access.ShareRecord, error) {
	rows, err := t.tx.QueryContext(ctx, `SELECT id,ownership_id,membership_id,permissions,expires_at FROM device_shares WHERE ownership_id=? ORDER BY id`, ownership)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []access.ShareRecord{}
	for rows.Next() {
		var share access.ShareRecord
		if err := rows.Scan(&share.ID, &share.OwnershipID, &share.MembershipID, &share.Permissions, &share.ExpiresAt); err != nil {
			return nil, err
		}
		result = append(result, share)
	}
	return result, rows.Err()
}
