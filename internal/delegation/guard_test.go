package delegation

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/xallor-ai/xallor-remote/internal/protocol"
)

// No process context is returned until the first bounded exchange succeeds;
// round-trip latency counts against the issued duration, not on top of it.
func TestGuard_should_rejectInitialDenialInvalidTTLAndLateReply(t *testing.T) {
	for _, kind := range []string{"denied", "zero", "oversize", "late"} {
		t.Run(kind, func(t *testing.T) {
			ctx, stop, err := Maintain(context.Background(), "proof", func(context.Context, string) (time.Duration, error) {
				switch kind {
				case "denied":
					return 0, ErrDenied
				case "zero":
					return 0, nil
				case "oversize":
					return protocol.ExecutionLeaseDuration + time.Second, nil
				default:
					time.Sleep(20 * time.Millisecond)
					return time.Millisecond, nil
				}
			})
			if err == nil || ctx != nil || stop != nil {
				t.Fatal("invalid authorization admitted", err)
			}
		})
	}
}

// Disconnects never extend authorization, while an authoritative revocation
// cancels immediately at the next exchange. Neither returns a success context.
func TestGuard_should_cancel_whenRenewalDeniedOrLastLeaseExpires(t *testing.T) {
	for _, denied := range []bool{false, true} {
		t.Run(map[bool]string{false: "offline", true: "revoked"}[denied], func(t *testing.T) {
			var calls atomic.Int32
			ctx, stop, err := Maintain(context.Background(), "proof", func(context.Context, string) (time.Duration, error) {
				if calls.Add(1) == 1 {
					return 90 * time.Millisecond, nil
				}
				if denied {
					return 0, ErrDenied
				}
				return 0, errors.New("offline")
			})
			if err != nil {
				t.Fatal(err)
			}
			defer stop()
			select {
			case <-ctx.Done():
			case <-time.After(time.Second):
				t.Fatal("authorization never expired")
			}
			want := ErrExpired
			if denied {
				want = ErrDenied
			}
			if !errors.Is(context.Cause(ctx), want) {
				t.Fatal(context.Cause(ctx))
			}
			if calls.Load() > 15 {
				t.Fatal("unbounded renewal retry loop", calls.Load())
			}
		})
	}
}

// Successful exchanges extend only a live lease. Parent revocation remains
// irreversible and must interrupt an in-flight network attempt.
func TestGuard_should_keepRenewing_untilParentIsCancelled(t *testing.T) {
	parent, cancel := context.WithCancelCause(context.Background())
	defer cancel(nil)
	third := make(chan struct{})
	var calls atomic.Int32
	ctx, stop, err := Maintain(parent, "proof", func(ctx context.Context, _ string) (time.Duration, error) {
		if calls.Add(1) == 3 {
			close(third)
			<-ctx.Done()
			return 0, ctx.Err()
		}
		return 90 * time.Millisecond, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	select {
	case <-third:
	case <-time.After(time.Second):
		t.Fatal("renewal not scheduled")
	}
	cause := errors.New("device changed")
	cancel(cause)
	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("parent cancellation lost")
	}
	if !errors.Is(context.Cause(ctx), cause) {
		t.Fatal(context.Cause(ctx))
	}
}

// Even a provider that delivers a nominal success after the previous lease's
// deadline cannot revive authority. Deadline expiry wins over the late TTL.
func TestGuard_should_notReviveExpiredLease_whenRenewalReturnsLateSuccess(t *testing.T) {
	var calls atomic.Int32
	ctx, stop, err := Maintain(context.Background(), "proof", func(context.Context, string) (time.Duration, error) {
		if calls.Add(1) == 1 {
			return 30 * time.Millisecond, nil
		}
		time.Sleep(50 * time.Millisecond)
		return time.Second, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("late reply revived expired authorization")
	}
	if !errors.Is(context.Cause(ctx), ErrExpired) {
		t.Fatal(context.Cause(ctx))
	}
}
