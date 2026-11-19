package relay

import (
	"testing"
	"time"

	"github.com/xallor-ai/xallor-remote/internal/accesscode"
)

// Evaluate the exact final-publication budget with a controlled clock: neither
// expiry at that instant nor sub-millisecond rounding may grant extra time.
func TestSessionExecutionLease_shouldNotExtendBudget_atFinalPublication(t *testing.T) {
	now := time.Unix(1800000000, 0).UTC()
	for _, tc := range []struct {
		name              string
		provider, session time.Duration
		want              int64
	}{
		{"session-expired", time.Second, 0, 0}, {"provider-expired", 0, time.Second, 0},
		{"session-sub-ms", time.Second, time.Millisecond - time.Nanosecond, 0},
		{"provider-sub-ms", time.Millisecond - time.Nanosecond, time.Second, 0},
		{"session-bound", time.Second, 500 * time.Millisecond, 500},
		{"provider-bound", 250 * time.Millisecond, time.Second, 250},
	} {
		t.Run(tc.name, func(t *testing.T) {
			expires := now.Add(tc.session)
			s := &accesscode.SessionAuthorization{ID: "s", CodeID: "c", Generation: 1, Scope: []string{"exec"}, IssuedAt: now.Add(-time.Minute), ExpiresAt: &expires, State: accesscode.SessionActive}
			if got := sessionExecutionLeaseMS(now.Add(tc.provider), s, now); got != tc.want {
				t.Fatalf("lease_ms=%d want=%d", got, tc.want)
			}
		})
	}
}
