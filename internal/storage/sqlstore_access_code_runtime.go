package storage

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/xallor-ai/xallor-remote/internal/accesscode"
)

var _ accesscode.Store = (*SQLStore)(nil)

type accessCodeTransaction struct{ tx *sql.Tx }

func (s *SQLStore) ReadAccessCode(ctx context.Context, fn func(accesscode.Reader) error) error {
	_, err := storeTransaction(ctx, s.db, func(tx *sql.Tx) (struct{}, error) {
		return struct{}{}, fn(accessCodeTransaction{tx: tx})
	})
	return err
}

func (s *SQLStore) WriteAccessCode(ctx context.Context, fn func(accesscode.Transaction) error) error {
	_, err := storeTransaction(ctx, s.db, func(tx *sql.Tx) (struct{}, error) {
		return struct{}{}, fn(accessCodeTransaction{tx: tx})
	})
	return err
}

func (t accessCodeTransaction) Code(ctx context.Context, id string) (accesscode.Code, error) {
	row := t.tx.QueryRowContext(ctx, `SELECT code_id,device_uid,ownership_id,type,generation,issued_at,expires_at,scope_json,require_approval,state FROM access_codes WHERE code_id=?`, id)
	var code accesscode.Code
	var typ, scope, state string
	var issued int64
	var expires sql.NullInt64
	var approval int
	if err := row.Scan(&code.ID, &code.DeviceUID, &code.OwnershipID, &typ, &code.Generation, &issued, &expires, &scope, &approval, &state); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return accesscode.Code{}, accesscode.ErrInvalid
		}
		return accesscode.Code{}, err
	}
	code.Type, code.State, code.IssuedAt = accesscode.Type(typ), accesscode.CodeState(state), time.Unix(issued, 0).UTC()
	code.RequireApproval = approval != 0
	if expires.Valid {
		value := time.Unix(expires.Int64, 0).UTC()
		code.ExpiresAt = &value
	}
	if err := json.Unmarshal([]byte(scope), &code.Scope); err != nil {
		return accesscode.Code{}, accesscode.ErrInvalid
	}
	if err := code.Validate(); err != nil {
		return accesscode.Code{}, err
	}
	return code, nil
}

func (t accessCodeTransaction) Reservation(ctx context.Context, id string) (accesscode.Reservation, error) {
	row := t.tx.QueryRowContext(ctx, `SELECT reservation_id,code_id,generation,session_id,state,reserved_at,reservation_expires_at,terminal_at,target_uid,target_epoch FROM code_consumptions WHERE reservation_id=?`, id)
	var reservation accesscode.Reservation
	var state string
	var reserved, expires int64
	var terminal sql.NullInt64
	if err := row.Scan(&reservation.ID, &reservation.CodeID, &reservation.Generation, &reservation.SessionID, &state, &reserved, &expires, &terminal, &reservation.Target.DeviceUID, &reservation.Target.Epoch); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return accesscode.Reservation{}, accesscode.ErrReservationState
		}
		return accesscode.Reservation{}, err
	}
	reservation.State = accesscode.ReservationState(state)
	reservation.ReservedAt = time.Unix(reserved, 0).UTC()
	reservation.ReservationExpiresAt = time.Unix(expires, 0).UTC()
	if terminal.Valid {
		value := time.Unix(terminal.Int64, 0).UTC()
		reservation.TerminalAt = &value
	}
	return reservation, nil
}

func (t accessCodeTransaction) Session(ctx context.Context, id string) (accesscode.SessionAuthorization, error) {
	row := t.tx.QueryRowContext(ctx, `SELECT session_id,code_id,generation,scope_json,issued_at,expires_at,state,target_uid,target_epoch FROM session_authorizations WHERE session_id=?`, id)
	var session accesscode.SessionAuthorization
	var scope, state string
	var issued int64
	var expires sql.NullInt64
	if err := row.Scan(&session.ID, &session.CodeID, &session.Generation, &scope, &issued, &expires, &state, &session.Target.DeviceUID, &session.Target.Epoch); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return accesscode.SessionAuthorization{}, accesscode.ErrSessionState
		}
		return accesscode.SessionAuthorization{}, err
	}
	session.State, session.IssuedAt = accesscode.SessionState(state), time.Unix(issued, 0).UTC()
	if expires.Valid {
		value := time.Unix(expires.Int64, 0).UTC()
		session.ExpiresAt = &value
	}
	if err := json.Unmarshal([]byte(scope), &session.Scope); err != nil {
		return accesscode.SessionAuthorization{}, accesscode.ErrInvalid
	}
	return session, nil
}

func (t accessCodeTransaction) InsertCode(ctx context.Context, code accesscode.Code, secretHash string) error {
	if err := code.Validate(); err != nil || strings.TrimSpace(secretHash) == "" {
		return accesscode.ErrInvalid
	}
	scope, err := json.Marshal(code.Scope)
	if err != nil {
		return err
	}
	var expires any
	if code.ExpiresAt != nil {
		expires = code.ExpiresAt.Unix()
	}
	_, err = t.tx.ExecContext(ctx, `INSERT INTO access_codes (code_id,device_uid,ownership_id,type,secret_hash,issued_at,expires_at,generation,scope_json,require_approval,state) VALUES (?,?,?,?,?,?,?,?,?,?,?)`, code.ID, code.DeviceUID, code.OwnershipID, code.Type, secretHash, code.IssuedAt.Unix(), expires, code.Generation, string(scope), boolInt(code.RequireApproval), code.State)
	return err
}

func (t accessCodeTransaction) ReserveCode(ctx context.Context, codeID, secretHash, reservationID, sessionID string, now, deadline time.Time, target accesscode.TargetBinding) (accesscode.Code, accesscode.Reservation, error) {
	var storedHash string
	if err := t.tx.QueryRowContext(ctx, `SELECT secret_hash FROM access_codes WHERE code_id=?`, codeID).Scan(&storedHash); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return accesscode.Code{}, accesscode.Reservation{}, accesscode.ErrInvalid
		}
		return accesscode.Code{}, accesscode.Reservation{}, err
	}
	if secretHash == "" || subtle.ConstantTimeCompare([]byte(storedHash), []byte(secretHash)) != 1 {
		return accesscode.Code{}, accesscode.Reservation{}, accesscode.ErrCodeSecret
	}
	code, err := t.Code(ctx, codeID)
	if err != nil {
		return accesscode.Code{}, accesscode.Reservation{}, err
	}
	if code.DeviceUID != target.DeviceUID {
		return accesscode.Code{}, accesscode.Reservation{}, accesscode.ErrTargetEpoch
	}
	if err := t.RequireSessionTarget(ctx, target); err != nil {
		return accesscode.Code{}, accesscode.Reservation{}, err
	}
	if code.Type == accesscode.OneTime {
		var count int
		if err := t.tx.QueryRowContext(ctx, `SELECT count(*) FROM code_consumptions WHERE code_id=? AND generation=? AND exclusive=1 AND state IN ('reserved','unknown')`, codeID, code.Generation).Scan(&count); err != nil {
			return accesscode.Code{}, accesscode.Reservation{}, err
		}
		if count != 0 {
			return accesscode.Code{}, accesscode.Reservation{}, accesscode.ErrReservationState
		}
	}
	reservation, err := code.Reserve(reservationID, sessionID, now, deadline)
	if err != nil {
		return accesscode.Code{}, accesscode.Reservation{}, err
	}
	reservation.Target = target
	_, err = t.tx.ExecContext(ctx, `INSERT INTO code_consumptions (reservation_id,code_id,generation,session_id,state,exclusive,reserved_at,reservation_expires_at,target_uid,target_epoch) VALUES (?,?,?,?,?,?,?,?,?,?)`, reservation.ID, reservation.CodeID, reservation.Generation, reservation.SessionID, reservation.State, boolInt(code.Type == accesscode.OneTime), reservation.ReservedAt.Unix(), reservation.ReservationExpiresAt.Unix(), target.DeviceUID, target.Epoch)
	if err != nil {
		return accesscode.Code{}, accesscode.Reservation{}, err
	}
	return code, reservation, nil
}

func (t accessCodeTransaction) FinalizeReservation(ctx context.Context, reservationID string, approved bool, now time.Time, sessionExpiresAt *time.Time, requestedScope []string) (accesscode.Code, accesscode.Reservation, accesscode.SessionAuthorization, error) {
	reservation, err := t.Reservation(ctx, reservationID)
	if err != nil {
		return accesscode.Code{}, accesscode.Reservation{}, accesscode.SessionAuthorization{}, err
	}
	if approved {
		if err := t.RequireSessionTarget(ctx, reservation.Target); err != nil {
			return accesscode.Code{}, accesscode.Reservation{}, accesscode.SessionAuthorization{}, err
		}
	}
	code, err := t.Code(ctx, reservation.CodeID)
	if err != nil {
		return accesscode.Code{}, accesscode.Reservation{}, accesscode.SessionAuthorization{}, err
	}
	nextCode, nextReservation, session, transitionErr := accesscode.Finalize(code, reservation, approved, now, sessionExpiresAt, requestedScope)
	if transitionErr != nil && nextReservation.State == accesscode.ReservationReserved {
		return accesscode.Code{}, accesscode.Reservation{}, accesscode.SessionAuthorization{}, transitionErr
	}
	var terminal any
	if nextReservation.TerminalAt != nil {
		terminal = nextReservation.TerminalAt.Unix()
	}
	if _, err := t.tx.ExecContext(ctx, `UPDATE code_consumptions SET state=?,terminal_at=? WHERE reservation_id=? AND state='reserved'`, nextReservation.State, terminal, nextReservation.ID); err != nil {
		return accesscode.Code{}, accesscode.Reservation{}, accesscode.SessionAuthorization{}, err
	}
	if nextCode.State != code.State {
		if _, err := t.tx.ExecContext(ctx, `UPDATE access_codes SET state=? WHERE code_id=? AND generation=?`, nextCode.State, nextCode.ID, nextCode.Generation); err != nil {
			return accesscode.Code{}, accesscode.Reservation{}, accesscode.SessionAuthorization{}, err
		}
	}
	if session.ID != "" {
		scope, err := json.Marshal(session.Scope)
		if err != nil {
			return accesscode.Code{}, accesscode.Reservation{}, accesscode.SessionAuthorization{}, err
		}
		var expires any
		if session.ExpiresAt != nil {
			expires = session.ExpiresAt.Unix()
		}
		if _, err := t.tx.ExecContext(ctx, `INSERT INTO session_authorizations (session_id,code_id,generation,scope_json,issued_at,expires_at,state,target_uid,target_epoch) VALUES (?,?,?,?,?,?,?,?,?)`, session.ID, session.CodeID, session.Generation, string(scope), session.IssuedAt.Unix(), expires, session.State, session.Target.DeviceUID, session.Target.Epoch); err != nil {
			return accesscode.Code{}, accesscode.Reservation{}, accesscode.SessionAuthorization{}, err
		}
	}
	// Rejection/expiry is an expected terminal transition. Its release must be
	// committed; returning the error to storeTransaction would roll it back and
	// leave the reservation retryable. Callers observe the released reservation.
	if transitionErr != nil && nextReservation.State == accesscode.ReservationReleased {
		return nextCode, nextReservation, session, nil
	}
	return nextCode, nextReservation, session, transitionErr
}

func (t accessCodeTransaction) MarkReservationUnknown(ctx context.Context, reservationID string, now time.Time) (accesscode.Reservation, error) {
	reservation, err := t.Reservation(ctx, reservationID)
	if err != nil {
		return accesscode.Reservation{}, err
	}
	next, err := reservation.MarkUnknown(now)
	if err != nil {
		return accesscode.Reservation{}, err
	}
	var terminal any
	if next.TerminalAt != nil {
		terminal = next.TerminalAt.Unix()
	}
	if _, err := t.tx.ExecContext(ctx, `UPDATE code_consumptions SET state=?,terminal_at=? WHERE reservation_id=?`, next.State, terminal, next.ID); err != nil {
		return accesscode.Reservation{}, err
	}
	return next, nil
}

func (t accessCodeTransaction) EndSession(ctx context.Context, sessionID string, now time.Time) (accesscode.SessionAuthorization, error) {
	return t.transitionSession(ctx, sessionID, now, false)
}

func (t accessCodeTransaction) RevokeSession(ctx context.Context, sessionID string, now time.Time) (accesscode.SessionAuthorization, error) {
	return t.transitionSession(ctx, sessionID, now, true)
}

func (t accessCodeTransaction) transitionSession(ctx context.Context, sessionID string, now time.Time, revoke bool) (accesscode.SessionAuthorization, error) {
	session, err := t.Session(ctx, sessionID)
	if err != nil {
		return accesscode.SessionAuthorization{}, err
	}
	var next accesscode.SessionAuthorization
	if revoke {
		next, err = session.Revoke(now)
	} else {
		next, err = session.End(now)
	}
	if err != nil {
		return accesscode.SessionAuthorization{}, err
	}
	var expires any
	if next.ExpiresAt != nil {
		expires = next.ExpiresAt.Unix()
	}
	if _, err := t.tx.ExecContext(ctx, `UPDATE session_authorizations SET state=?,expires_at=? WHERE session_id=? AND state='active'`, next.State, expires, next.ID); err != nil {
		return accesscode.SessionAuthorization{}, err
	}
	if _, err := t.tx.ExecContext(ctx, `UPDATE resume_capabilities SET state='revoked' WHERE session_id=? AND state='active'`, next.ID); err != nil {
		return accesscode.SessionAuthorization{}, err
	}
	return next, nil
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}
