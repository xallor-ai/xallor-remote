package delegation

import (
	"context"
	"errors"
	"time"

	"github.com/xallor-ai/xallor-remote/internal/protocol"
)

type Renew func(context.Context, string) (time.Duration, error)

// Maintain acquires fresh authorization before returning a usable context.
// Its deadline starts before each round trip, so delayed replies cannot extend
// execution authority. Network errors never extend the last confirmed lease.
func Maintain(parent context.Context, proof string, renew Renew) (context.Context, context.CancelFunc, error) {
	ctx, cancel := context.WithCancelCause(parent)
	if renew == nil {
		cancel(ErrDenied)
		return nil, nil, ErrDenied
	}
	request := func(deadline time.Time) (time.Time, error) {
		start := time.Now()
		if limit := start.Add(protocol.ExecutionRenewTimeout); limit.Before(deadline) {
			deadline = limit
		}
		attempt, stop := context.WithDeadline(ctx, deadline)
		defer stop()
		ttl, err := renew(attempt, proof)
		if err != nil {
			return time.Time{}, err
		}
		if attempt.Err() != nil {
			return time.Time{}, attempt.Err()
		}
		if !time.Now().Before(deadline) {
			return time.Time{}, ErrExpired
		}
		if ttl <= 0 || ttl > protocol.ExecutionLeaseDuration {
			return time.Time{}, ErrDenied
		}
		expires := start.Add(ttl)
		if !expires.After(time.Now()) {
			return time.Time{}, ErrExpired
		}
		return expires, nil
	}
	expires, err := request(time.Now().Add(protocol.ExecutionRenewTimeout))
	if err != nil {
		cancel(err)
		return nil, nil, err
	}
	go func() {
		defer cancel(nil)
		for {
			remaining := time.Until(expires)
			if remaining <= 0 {
				cancel(ErrExpired)
				return
			}
			wait := remaining / 3
			if wait > protocol.ExecutionRenewInterval {
				wait = protocol.ExecutionRenewInterval
			}
			if wait < 10*time.Millisecond {
				wait = 10 * time.Millisecond
			}
			if wait > remaining {
				wait = remaining
			}
			timer := time.NewTimer(wait)
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
			next, err := request(expires)
			if err == nil {
				expires = next
				continue
			}
			if errors.Is(err, ErrDenied) {
				cancel(ErrDenied)
				return
			}
			if ctx.Err() != nil {
				return
			}
			// Failed requests retry with the old expiry; no grace is added.
		}
	}()
	return ctx, func() { cancel(context.Canceled) }, nil
}
