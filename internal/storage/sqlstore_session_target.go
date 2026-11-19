package storage

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/xallor-ai/xallor-remote/internal/accesscode"
)

func (t accessCodeTransaction) RequireSessionTarget(ctx context.Context, expected accesscode.TargetBinding) error {
	if !expected.Valid() {
		return accesscode.ErrTargetEpoch
	}
	var epoch string
	var active bool
	err := t.tx.QueryRowContext(ctx, `SELECT epoch,active FROM session_targets WHERE device_uid=?`, expected.DeviceUID).Scan(&epoch, &active)
	if errors.Is(err, sql.ErrNoRows) {
		return accesscode.ErrTargetEpoch
	}
	if err != nil {
		return err
	}
	if !active || epoch != expected.Epoch {
		return accesscode.ErrTargetEpoch
	}
	return nil
}

func (t accessCodeTransaction) ActivateSessionTarget(ctx context.Context, next accesscode.TargetBinding, now time.Time) error {
	if !next.Valid() {
		return accesscode.ErrTargetEpoch
	}
	var old string
	err := t.tx.QueryRowContext(ctx, `SELECT epoch FROM session_targets WHERE device_uid=?`, next.DeviceUID).Scan(&old)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if err == nil && old == next.Epoch {
		return accesscode.ErrTargetEpoch
	}
	if _, err := t.tx.ExecContext(ctx, `INSERT INTO session_targets(device_uid,epoch,active) VALUES(?,?,1)
ON CONFLICT(device_uid) DO UPDATE SET epoch=excluded.epoch,active=1`, next.DeviceUID, next.Epoch); err != nil {
		return err
	}
	return t.retireTargetRows(ctx, next, now, true)
}

func (t accessCodeTransaction) RetireSessionTarget(ctx context.Context, expected accesscode.TargetBinding, now time.Time) error {
	if !expected.Valid() {
		return accesscode.ErrTargetEpoch
	}
	r, err := t.tx.ExecContext(ctx, `UPDATE session_targets SET active=0 WHERE device_uid=? AND epoch=? AND active=1`, expected.DeviceUID, expected.Epoch)
	if err != nil {
		return err
	}
	n, err := r.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return nil
	} // Repeated or stale retirement cannot touch a replacement.
	return t.retireTargetRows(ctx, expected, now, false)
}

func (t accessCodeTransaction) retireTargetRows(ctx context.Context, b accesscode.TargetBinding, now time.Time, other bool) error {
	comparison := "="
	if other {
		comparison = "<>"
	}
	where := `target_uid=? AND target_epoch ` + comparison + ` ?`
	if _, err := t.tx.ExecContext(ctx, `UPDATE resume_capabilities SET state='revoked' WHERE state='active' AND session_id IN (SELECT session_id FROM session_authorizations WHERE `+where+`)`, b.DeviceUID, b.Epoch); err != nil {
		return err
	}
	if _, err := t.tx.ExecContext(ctx, `UPDATE session_authorizations SET state='revoked' WHERE state='active' AND `+where, b.DeviceUID, b.Epoch); err != nil {
		return err
	}
	_, err := t.tx.ExecContext(ctx, `UPDATE code_consumptions SET state='unknown',terminal_at=? WHERE state='reserved' AND `+where, now.Unix(), b.DeviceUID, b.Epoch)
	return err
}
