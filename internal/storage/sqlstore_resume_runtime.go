package storage

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"errors"
	"time"

	"github.com/xallor-ai/xallor-remote/internal/accesscode"
)

func (t accessCodeTransaction) Resume(ctx context.Context, id string) (accesscode.ResumeCapability, error) {
	row := t.tx.QueryRowContext(ctx, `SELECT resume_id,session_id,binding_kind,binding_digest,capability_sha256,issued_at,expires_at,generation,state FROM resume_capabilities WHERE resume_id=?`, id)
	var resume accesscode.ResumeCapability
	var issued, expires int64
	var state string
	if err := row.Scan(&resume.ID, &resume.SessionID, &resume.BindingKind, &resume.BindingDigest, &resume.CapabilityHash, &issued, &expires, &resume.Generation, &state); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return accesscode.ResumeCapability{}, accesscode.ErrResumeInvalid
		}
		return accesscode.ResumeCapability{}, err
	}
	resume.IssuedAt, resume.ExpiresAt = time.Unix(issued, 0).UTC(), time.Unix(expires, 0).UTC()
	resume.State = accesscode.ResumeState(state)
	if err := resume.Validate(); err != nil {
		return accesscode.ResumeCapability{}, err
	}
	return resume, nil
}

func (t accessCodeTransaction) ActiveResumeForSession(ctx context.Context, sessionID string) (accesscode.ResumeCapability, error) {
	row := t.tx.QueryRowContext(ctx, `SELECT resume_id FROM resume_capabilities WHERE session_id=? AND state='active' ORDER BY issued_at DESC LIMIT 1`, sessionID)
	var id string
	if err := row.Scan(&id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return accesscode.ResumeCapability{}, accesscode.ErrResumeInvalid
		}
		return accesscode.ResumeCapability{}, err
	}
	return t.Resume(ctx, id)
}

func (t accessCodeTransaction) InsertResume(ctx context.Context, resume accesscode.ResumeCapability) error {
	if err := resume.Validate(); err != nil {
		return err
	}
	session, err := t.Session(ctx, resume.SessionID)
	if err != nil {
		return err
	}
	if err := session.CanOperate(resume.IssuedAt); err != nil {
		return err
	}
	if err := t.RequireSessionTarget(ctx, session.Target); err != nil {
		return err
	}
	_, err = t.tx.ExecContext(ctx, `INSERT INTO resume_capabilities (resume_id,session_id,binding_kind,binding_digest,capability_sha256,issued_at,expires_at,generation,state) VALUES (?,?,?,?,?,?,?,?,?)`, resume.ID, resume.SessionID, resume.BindingKind, resume.BindingDigest, resume.CapabilityHash, resume.IssuedAt.Unix(), resume.ExpiresAt.Unix(), resume.Generation, resume.State)
	return err
}

func (t accessCodeTransaction) ConsumeResume(ctx context.Context, id, secretHash string, now time.Time, sessionID, bindingKind, bindingDigest string) (accesscode.ResumeCapability, error) {
	resume, err := t.Resume(ctx, id)
	if err != nil {
		return accesscode.ResumeCapability{}, err
	}
	if subtle.ConstantTimeCompare([]byte(resume.CapabilityHash), []byte(secretHash)) != 1 {
		return accesscode.ResumeCapability{}, accesscode.ErrResumeSecret
	}
	session, err := t.Session(ctx, sessionID)
	if err != nil {
		return accesscode.ResumeCapability{}, err
	}
	if err := session.CanOperate(now); err != nil {
		return accesscode.ResumeCapability{}, err
	}
	if err := t.RequireSessionTarget(ctx, session.Target); err != nil {
		return accesscode.ResumeCapability{}, err
	}
	if err := resume.CanUse(now, sessionID, bindingKind, bindingDigest); err != nil {
		return accesscode.ResumeCapability{}, err
	}
	next, err := resume.Consume(now)
	if err != nil {
		return accesscode.ResumeCapability{}, err
	}
	result, err := t.tx.ExecContext(ctx, `UPDATE resume_capabilities SET state='consumed' WHERE resume_id=? AND state='active'`, id)
	if err != nil {
		return accesscode.ResumeCapability{}, err
	}
	if count, err := result.RowsAffected(); err != nil || count != 1 {
		return accesscode.ResumeCapability{}, accesscode.ErrResumeState
	}
	return next, nil
}

func (t accessCodeTransaction) RevokeResume(ctx context.Context, id string, now time.Time) (accesscode.ResumeCapability, error) {
	resume, err := t.Resume(ctx, id)
	if err != nil {
		return accesscode.ResumeCapability{}, err
	}
	next, err := resume.Revoke(now)
	if err != nil {
		return accesscode.ResumeCapability{}, err
	}
	if _, err := t.tx.ExecContext(ctx, `UPDATE resume_capabilities SET state='revoked' WHERE resume_id=? AND state='active'`, id); err != nil {
		return accesscode.ResumeCapability{}, err
	}
	return next, nil
}

func (t accessCodeTransaction) MarkResumeUnknown(ctx context.Context, id string, now time.Time) (accesscode.ResumeCapability, error) {
	resume, err := t.Resume(ctx, id)
	if err != nil {
		return accesscode.ResumeCapability{}, err
	}
	next, err := resume.MarkUnknown(now)
	if err != nil {
		return accesscode.ResumeCapability{}, err
	}
	if _, err := t.tx.ExecContext(ctx, `UPDATE resume_capabilities SET state='unknown' WHERE resume_id=? AND state='active'`, id); err != nil {
		return accesscode.ResumeCapability{}, err
	}
	return next, nil
}
