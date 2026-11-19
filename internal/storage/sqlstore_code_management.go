package storage

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"errors"
	"math"
	"time"

	"github.com/xallor-ai/xallor-remote/internal/accesscode"
)

func (t accessCodeTransaction) Rotation(ctx context.Context, id string) (accesscode.Rotation, error) {
	var r accesscode.Rotation
	err := t.tx.QueryRowContext(ctx, `SELECT old_code_id,new_code_id,intent_sha256 FROM code_rotations WHERE old_code_id=?`, id).Scan(&r.PreviousCodeID, &r.ReplacementCodeID, &r.IntentSHA256)
	if errors.Is(err, sql.ErrNoRows) {
		return r, accesscode.ErrRotationNotFound
	}
	return r, err
}

func (t accessCodeTransaction) RevokeCode(ctx context.Context, codeID string) (accesscode.Code, error) {
	return t.revokeCodeAt(ctx, codeID, time.Now().UTC())
}

func (t accessCodeTransaction) revokeCodeAt(ctx context.Context, codeID string, now time.Time) (accesscode.Code, error) {
	code, err := t.Code(ctx, codeID)
	if err != nil {
		return accesscode.Code{}, err
	}
	next, err := code.Revoke()
	if err != nil {
		return accesscode.Code{}, err
	}
	if _, err := t.tx.ExecContext(ctx, `UPDATE access_codes SET state='revoked' WHERE code_id=? AND generation=? AND state<>'revoked'`, code.ID, code.Generation); err != nil {
		return accesscode.Code{}, err
	}
	if err := t.revokeCodeChildren(ctx, code.ID, code.Generation, now); err != nil {
		return accesscode.Code{}, err
	}
	return next, nil
}

func (t accessCodeTransaction) revokeCodeChildren(ctx context.Context, id string, generation uint64, now time.Time) error {
	if _, err := t.tx.ExecContext(ctx, `UPDATE resume_capabilities SET state='revoked' WHERE state='active' AND session_id IN (SELECT session_id FROM session_authorizations WHERE code_id=? AND generation=?)`, id, generation); err != nil {
		return err
	}
	if _, err := t.tx.ExecContext(ctx, `UPDATE session_authorizations SET state='revoked' WHERE code_id=? AND generation=? AND state IN ('active','pending')`, id, generation); err != nil {
		return err
	}
	_, err := t.tx.ExecContext(ctx, `UPDATE code_consumptions SET state='unknown',terminal_at=? WHERE code_id=? AND generation=? AND state='reserved'`, now.Unix(), id, generation)
	return err
}

func (t accessCodeTransaction) RotateCode(ctx context.Context, oldID, newID, secretHash string, now time.Time) (accesscode.Code, error) {
	digest, err := accesscode.RotationDigest(oldID, newID, secretHash)
	if err != nil {
		return accesscode.Code{}, err
	}
	receipt, err := t.Rotation(ctx, oldID)
	if err == nil {
		if receipt.ReplacementCodeID != newID || subtle.ConstantTimeCompare([]byte(receipt.IntentSHA256), []byte(digest)) != 1 {
			return accesscode.Code{}, accesscode.ErrRotationConflict
		}
		old, err := t.Code(ctx, oldID)
		if err != nil {
			return accesscode.Code{}, err
		}
		next, err := t.Code(ctx, newID)
		if err != nil {
			return accesscode.Code{}, err
		}
		if old.State != accesscode.StateRevoked || next.DeviceUID != old.DeviceUID || next.OwnershipID != old.OwnershipID || next.Generation <= old.Generation {
			return accesscode.Code{}, accesscode.ErrRotationConflict
		}
		return next, nil // A receipt observes current metadata; it never reactivates.
	}
	if !errors.Is(err, accesscode.ErrRotationNotFound) {
		return accesscode.Code{}, err
	}
	old, err := t.Code(ctx, oldID)
	if err != nil {
		return accesscode.Code{}, err
	}
	var oldHash string
	if err := t.tx.QueryRowContext(ctx, `SELECT secret_hash FROM access_codes WHERE code_id=?`, oldID).Scan(&oldHash); err != nil {
		return accesscode.Code{}, err
	}
	if subtle.ConstantTimeCompare([]byte(oldHash), []byte(secretHash)) == 1 {
		return accesscode.Code{}, accesscode.ErrRotationConflict
	}
	var exists int
	if err := t.tx.QueryRowContext(ctx, `SELECT count(*) FROM access_codes WHERE code_id=?`, newID).Scan(&exists); err != nil {
		return accesscode.Code{}, err
	}
	if exists != 0 {
		return accesscode.Code{}, accesscode.ErrRotationConflict
	}
	if old.Type == accesscode.OneTime {
		var occupied int
		if err := t.tx.QueryRowContext(ctx, `SELECT
(SELECT count(*) FROM code_consumptions WHERE code_id=? AND generation=? AND state IN ('reserved','consumed','unknown'))+
(SELECT count(*) FROM session_authorizations WHERE code_id=? AND generation=?)`, oldID, old.Generation, oldID, old.Generation).Scan(&occupied); err != nil {
			return accesscode.Code{}, err
		}
		if occupied != 0 {
			return accesscode.Code{}, accesscode.ErrReservationState
		}
	}
	var maximum int64
	if err := t.tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(generation),0) FROM access_codes WHERE device_uid=?`, old.DeviceUID).Scan(&maximum); err != nil {
		return accesscode.Code{}, err
	}
	if maximum == math.MaxInt64 {
		return accesscode.Code{}, accesscode.ErrInvalid
	}
	next, err := old.Replacement(newID, uint64(maximum+1), now)
	if err != nil {
		return accesscode.Code{}, err
	}
	if _, err := t.revokeCodeAt(ctx, oldID, now); err != nil {
		return accesscode.Code{}, err
	}
	if err := t.InsertCode(ctx, next, secretHash); err != nil {
		return accesscode.Code{}, err
	}
	if _, err := t.tx.ExecContext(ctx, `INSERT INTO code_rotations(old_code_id,new_code_id,intent_sha256) VALUES(?,?,?)`, oldID, newID, digest); err != nil {
		return accesscode.Code{}, err
	}
	return next, nil
}
