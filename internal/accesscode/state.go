// Package accesscode contains the pure lifecycle rules for access codes.
// Persistence, transport and recipient approval are owned by callers; this
// package never stores or logs a plaintext bearer code.
package accesscode

import (
	"errors"
	"time"
)

type Type string

const (
	OneTime   Type = "one_time"
	Expiring  Type = "expiring"
	LongLived Type = "long_lived"
)

type CodeState string

const (
	StateActive   CodeState = "active"
	StateRevoked  CodeState = "revoked"
	StateExpired  CodeState = "expired"
	StateConsumed CodeState = "consumed"
)

type ReservationState string

const (
	ReservationReserved ReservationState = "reserved"
	ReservationConsumed ReservationState = "consumed"
	ReservationReleased ReservationState = "released"
	ReservationUnknown  ReservationState = "unknown"
)

type SessionState string

const (
	SessionPending SessionState = "pending"
	SessionActive  SessionState = "active"
	SessionEnded   SessionState = "ended"
	SessionRevoked SessionState = "revoked"
	SessionUnknown SessionState = "unknown"
)

var (
	ErrInvalid            = errors.New("access code state is invalid")
	ErrCodeExpired        = errors.New("access code is expired")
	ErrCodeRevoked        = errors.New("access code is revoked")
	ErrCodeConsumed       = errors.New("access code is consumed")
	ErrReservationExpired = errors.New("access code reservation is expired")
	ErrReservationState   = errors.New("access code reservation state is invalid")
	ErrSessionState       = errors.New("access code session state is invalid")
	ErrSessionExpired     = errors.New("access code session authorization is expired")
	ErrCodeSecret         = errors.New("access code secret is invalid")
)

type Code struct {
	ID              string     `json:"code_id"`
	DeviceUID       string     `json:"device_uid,omitempty"`
	OwnershipID     string     `json:"ownership_id,omitempty"`
	Type            Type       `json:"type"`
	Generation      uint64     `json:"generation"`
	IssuedAt        time.Time  `json:"issued_at"`
	ExpiresAt       *time.Time `json:"expires_at,omitempty"`
	Scope           []string   `json:"scope"`
	RequireApproval bool       `json:"require_recipient_approval"`
	State           CodeState  `json:"state"`
}

type Reservation struct {
	Target               TargetBinding    `json:"target"`
	ID                   string           `json:"reservation_id"`
	CodeID               string           `json:"code_id"`
	Generation           uint64           `json:"generation"`
	SessionID            string           `json:"session_id"`
	State                ReservationState `json:"state"`
	ReservedAt           time.Time        `json:"reserved_at"`
	ReservationExpiresAt time.Time        `json:"reservation_expires_at"`
	TerminalAt           *time.Time       `json:"terminal_at,omitempty"`
}

type SessionAuthorization struct {
	Target     TargetBinding `json:"target"`
	ID         string        `json:"session_id"`
	CodeID     string        `json:"code_id"`
	Generation uint64        `json:"generation"`
	Scope      []string      `json:"scope"`
	IssuedAt   time.Time     `json:"issued_at"`
	ExpiresAt  *time.Time    `json:"expires_at,omitempty"`
	State      SessionState  `json:"state"`
}

func (c Code) normalized() Code {
	if c.State == "" {
		c.State = StateActive
	}
	c.Scope = append([]string(nil), c.Scope...)
	return c
}

func (c Code) Validate() error {
	if c.ID == "" || c.Generation == 0 || c.IssuedAt.IsZero() {
		return ErrInvalid
	}
	switch c.Type {
	case OneTime, LongLived:
		if c.ExpiresAt != nil {
			return ErrInvalid
		}
	case Expiring:
		if c.ExpiresAt == nil || !c.ExpiresAt.After(c.IssuedAt) {
			return ErrInvalid
		}
	default:
		return ErrInvalid
	}
	switch c.State {
	case StateActive, StateRevoked, StateExpired, StateConsumed:
	default:
		return ErrInvalid
	}
	return nil
}

func (c Code) redemptionError(now time.Time) error {
	c = c.normalized()
	if err := c.Validate(); err != nil {
		return err
	}
	switch c.State {
	case StateRevoked:
		return ErrCodeRevoked
	case StateExpired:
		return ErrCodeExpired
	case StateConsumed:
		return ErrCodeConsumed
	}
	if c.ExpiresAt != nil && !now.Before(*c.ExpiresAt) {
		return ErrCodeExpired
	}
	return nil
}

// Reserve records a bounded attempt without consuming a one-time code. The
// persistence adapter must enforce uniqueness for concurrent reservations.
func (c Code) Reserve(id, sessionID string, now, deadline time.Time) (Reservation, error) {
	c = c.normalized()
	if err := c.redemptionError(now); err != nil {
		return Reservation{}, err
	}
	if id == "" || sessionID == "" || now.IsZero() || deadline.IsZero() || !deadline.After(now) {
		return Reservation{}, ErrInvalid
	}
	if c.ExpiresAt != nil && deadline.After(*c.ExpiresAt) {
		deadline = *c.ExpiresAt
	}
	if !deadline.After(now) {
		return Reservation{}, ErrReservationExpired
	}
	return Reservation{ID: id, CodeID: c.ID, Generation: c.Generation, SessionID: sessionID,
		State: ReservationReserved, ReservedAt: now.UTC(), ReservationExpiresAt: deadline.UTC()}, nil
}

// Finalize performs the pure final transition after recipient approval. The
// caller must persist all returned values in one transaction.
func Finalize(c Code, r Reservation, approved bool, now time.Time, sessionExpiresAt *time.Time, requestedScope []string) (Code, Reservation, SessionAuthorization, error) {
	c = c.normalized()
	if err := c.Validate(); err != nil {
		return Code{}, Reservation{}, SessionAuthorization{}, err
	}
	if r.State != ReservationReserved || r.ID == "" || r.CodeID != c.ID || r.Generation != c.Generation || r.SessionID == "" {
		return Code{}, Reservation{}, SessionAuthorization{}, ErrReservationState
	}
	if now.IsZero() || !now.Before(r.ReservationExpiresAt) {
		return c, terminalReservation(r, ReservationReleased, now), SessionAuthorization{}, ErrReservationExpired
	}
	if err := c.redemptionError(now); err != nil {
		return c, terminalReservation(r, ReservationReleased, now), SessionAuthorization{}, err
	}
	if !approved {
		return c, terminalReservation(r, ReservationReleased, now), SessionAuthorization{}, nil
	}
	scope, err := c.ScopeForSession(r.Generation, requestedScope)
	if err != nil {
		return c, terminalReservation(r, ReservationReleased, now), SessionAuthorization{}, err
	}
	if sessionExpiresAt != nil && !sessionExpiresAt.After(now) {
		return c, terminalReservation(r, ReservationReleased, now), SessionAuthorization{}, ErrInvalid
	}
	if c.Type == OneTime {
		c.State = StateConsumed
	}
	r = terminalReservation(r, ReservationConsumed, now)
	session := SessionAuthorization{ID: r.SessionID, CodeID: c.ID, Generation: c.Generation, Target: r.Target,
		Scope: scope, IssuedAt: now.UTC(), State: SessionActive}
	if sessionExpiresAt != nil {
		expires := sessionExpiresAt.UTC()
		session.ExpiresAt = &expires
	}
	return c, r, session, nil
}

func terminalReservation(r Reservation, state ReservationState, now time.Time) Reservation {
	r.State = state
	if !now.IsZero() {
		t := now.UTC()
		r.TerminalAt = &t
	}
	return r
}

// MarkUnknown preserves an inconclusive final response. It never releases a
// reservation or returns a one-time code to the redeemable pool.
func (r Reservation) MarkUnknown(now time.Time) (Reservation, error) {
	if r.State != ReservationReserved && r.State != ReservationConsumed {
		return Reservation{}, ErrReservationState
	}
	return terminalReservation(r, ReservationUnknown, now), nil
}

func (c Code) Revoke() (Code, error) {
	c = c.normalized()
	if err := c.Validate(); err != nil {
		return Code{}, err
	}
	// Revocation is idempotent and remains available after consumption/expiry.
	// Consumption history is a separate reservation fact, never refunded here.
	c.State = StateRevoked
	return c, nil
}

func (s SessionAuthorization) CanOperate(now time.Time) error {
	if s.ID == "" || s.CodeID == "" || s.Generation == 0 || s.IssuedAt.IsZero() {
		return ErrInvalid
	}
	if s.State != SessionActive {
		return ErrSessionState
	}
	if s.ExpiresAt != nil && !now.Before(*s.ExpiresAt) {
		return ErrSessionExpired
	}
	return nil
}

func (s SessionAuthorization) Allows(operation string) bool {
	for _, allowed := range s.Scope {
		if allowed == operation {
			return true
		}
	}
	return false
}

func (s SessionAuthorization) End(now time.Time) (SessionAuthorization, error) {
	if s.State != SessionActive {
		return SessionAuthorization{}, ErrSessionState
	}
	s.State = SessionEnded
	if !now.IsZero() {
		s.ExpiresAt = timePtr(now)
	}
	return s, nil
}

func (s SessionAuthorization) Revoke(now time.Time) (SessionAuthorization, error) {
	if s.State != SessionActive {
		return SessionAuthorization{}, ErrSessionState
	}
	s.State = SessionRevoked
	if !now.IsZero() {
		s.ExpiresAt = timePtr(now)
	}
	return s, nil
}

func timePtr(t time.Time) *time.Time {
	u := t.UTC()
	return &u
}
