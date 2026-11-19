package accesscode

import (
	"errors"
	"testing"
	"time"
)

var accessCodeNow = time.Date(2026, 11, 17, 8, 0, 0, 0, time.UTC)

func codeFixture(t *testing.T, kind Type) Code {
	t.Helper()
	return Code{ID: "code-1", Type: kind, Generation: 3, IssuedAt: accessCodeNow.Add(-time.Minute), Scope: []string{"info", "exec"}, RequireApproval: true, State: StateActive}
}

// One-time codes are consumed only after final approval, never at reservation.
func TestCode_shouldConsumeOneTimeOnlyAfterFinalApproval(t *testing.T) {
	code := codeFixture(t, OneTime)
	reservation, err := code.Reserve("reservation-1", "session-1", accessCodeNow, accessCodeNow.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if code.State != StateActive || reservation.State != ReservationReserved {
		t.Fatalf("reservation changed code: code=%+v reservation=%+v", code, reservation)
	}
	code, reservation, session, err := Finalize(code, reservation, false, accessCodeNow.Add(10*time.Second), nil, code.Scope)
	if err != nil || reservation.State != ReservationReleased || session.State != "" || code.State != StateActive {
		t.Fatalf("rejected reservation=%+v session=%+v code=%+v err=%v", reservation, session, code, err)
	}
	reservation, err = code.Reserve("reservation-2", "session-2", accessCodeNow.Add(20*time.Second), accessCodeNow.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	code, reservation, session, err = Finalize(code, reservation, true, accessCodeNow.Add(30*time.Second), nil, code.Scope)
	if err != nil || code.State != StateConsumed || reservation.State != ReservationConsumed || session.State != SessionActive {
		t.Fatalf("approved code=%+v reservation=%+v session=%+v err=%v", code, reservation, session, err)
	}
	if _, err = code.Reserve("reservation-3", "session-3", accessCodeNow.Add(40*time.Second), accessCodeNow.Add(2*time.Minute)); !errors.Is(err, ErrCodeConsumed) {
		t.Fatalf("reused one-time code error=%v", err)
	}
}

// Expiring codes reject at the authoritative boundary but do not end an active session.
func TestCode_shouldRejectNewSessionAtExpiry_andKeepSessionIndependent(t *testing.T) {
	code := codeFixture(t, Expiring)
	expires := accessCodeNow.Add(time.Minute)
	code.ExpiresAt = &expires
	if _, err := code.Reserve("reservation-before", "session-before", expires.Add(-time.Nanosecond), expires); err != nil {
		t.Fatal(err)
	}
	if _, err := code.Reserve("reservation-at", "session-at", expires, expires.Add(time.Minute)); !errors.Is(err, ErrCodeExpired) {
		t.Fatalf("boundary reserve error=%v", err)
	}
	reservation, err := code.Reserve("reservation-live", "session-live", expires.Add(-time.Second), expires)
	if err != nil {
		t.Fatal(err)
	}
	sessionExpiry := expires.Add(10 * time.Minute)
	_, reservation, session, err := Finalize(code, reservation, true, expires.Add(-time.Second), &sessionExpiry, code.Scope)
	if err != nil || reservation.State != ReservationConsumed || session.State != SessionActive {
		t.Fatalf("session finalize reservation=%+v session=%+v err=%v", reservation, session, err)
	}
	if err := session.CanOperate(expires.Add(time.Second)); err != nil {
		t.Fatalf("code expiry ended existing session: %v", err)
	}
}

// Long-lived codes have no automatic expiry, but approval remains mandatory.
func TestCode_shouldKeepLongLivedRedeemable_untilRevoked(t *testing.T) {
	code := codeFixture(t, LongLived)
	longLivedNow := accessCodeNow.Add(90 * 24 * time.Hour)
	reservation, err := code.Reserve("reservation-1", "session-1", longLivedNow, longLivedNow.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	code, reservation, session, err := Finalize(code, reservation, true, longLivedNow.Add(time.Second), nil, code.Scope)
	if err != nil || code.State != StateActive || reservation.State != ReservationConsumed || session.State != SessionActive {
		t.Fatalf("long-lived finalize code=%+v reservation=%+v session=%+v err=%v", code, reservation, session, err)
	}
	if _, err := code.Reserve("reservation-2", "session-2", longLivedNow, longLivedNow.Add(time.Minute)); err != nil {
		t.Fatal("long-lived code unexpectedly expired:", err)
	}
	code, err = code.Revoke()
	if err != nil || code.State != StateRevoked {
		t.Fatalf("revoke code=%+v err=%v", code, err)
	}
	if _, err := code.Reserve("reservation-3", "session-3", accessCodeNow, accessCodeNow.Add(time.Minute)); !errors.Is(err, ErrCodeRevoked) {
		t.Fatalf("revoked code error=%v", err)
	}
}

// A lost final response is represented as unknown and cannot be refunded.
func TestReservation_shouldPreserveUnknown_withoutRefundingConsumption(t *testing.T) {
	code := codeFixture(t, OneTime)
	reservation, err := code.Reserve("reservation-1", "session-1", accessCodeNow, accessCodeNow.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	code, reservation, _, err = Finalize(code, reservation, true, accessCodeNow.Add(time.Second), nil, code.Scope)
	if err != nil || code.State != StateConsumed {
		t.Fatal(code, err)
	}
	reservation, err = reservation.MarkUnknown(accessCodeNow.Add(2 * time.Second))
	if err != nil || reservation.State != ReservationUnknown {
		t.Fatalf("unknown reservation=%+v err=%v", reservation, err)
	}
	if _, err := code.Reserve("reservation-retry", "session-retry", accessCodeNow.Add(3*time.Second), accessCodeNow.Add(time.Minute)); !errors.Is(err, ErrCodeConsumed) {
		t.Fatalf("unknown result refunded code: %v", err)
	}
}

// A rotated generation cannot finalize a reservation created from the prior code.
func TestReservation_shouldRejectGenerationMismatch(t *testing.T) {
	code := codeFixture(t, OneTime)
	reservation, err := code.Reserve("reservation-1", "session-1", accessCodeNow, accessCodeNow.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	code.Generation++
	if _, _, _, err := Finalize(code, reservation, true, accessCodeNow.Add(time.Second), nil, code.Scope); !errors.Is(err, ErrReservationState) {
		t.Fatalf("generation mismatch error=%v", err)
	}
}
