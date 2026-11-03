package noderun

import (
	"context"
	"errors"
	"time"

	"github.com/xallor-ai/xallor-remote/internal/delegation"
	"github.com/xallor-ai/xallor-remote/internal/protocol"
)

// No process has started: transport uncertainty must not be reported as a
// target failure, and caller cancellation/deadline retain their own meaning.
func executionLeaseStartCode(ctx context.Context, err error) string {
	if ctx.Err() != nil {
		return startFailureCode(ctx)
	}
	if errors.Is(err, delegation.ErrDenied) {
		return protocol.Unauthorized
	}
	return protocol.RelayError
}

func (d *Daemon) renewExecution(ctx context.Context, proof string) (time.Duration, error) {
	if proof == "" || len(proof) > protocol.MaxDelegationBytes {
		return 0, delegation.ErrDenied
	}
	return d.requestExecutionLease(ctx, protocol.Message{Type: protocol.TypeExecutionRenew, Delegation: proof})
}

func (d *Daemon) renewForeground(ctx context.Context, routeID string) (time.Duration, error) {
	if routeID == "" {
		return 0, delegation.ErrDenied
	}
	return d.requestExecutionLease(ctx, protocol.Message{Type: protocol.TypeExecutionRenewRoute, ExecID: routeID})
}

func (d *Daemon) requestExecutionLease(ctx context.Context, request protocol.Message) (time.Duration, error) {
	d.mu.Lock()
	link := d.device
	d.mu.Unlock()
	if link == nil {
		return 0, errStr("device disconnected")
	}
	key, replies, remove, err := d.executionReplies.register(link)
	if err != nil {
		return 0, err
	}
	defer remove()
	request.RequestID = key
	if err = link.sendContext(ctx, request); err != nil {
		return 0, err
	}
	select {
	case reply := <-replies:
		if reply.Code == protocol.Unauthorized {
			return 0, delegation.ErrDenied
		}
		if reply.Code != "" {
			return 0, errStr(reply.Code)
		}
		if reply.LeaseMS <= 0 || reply.LeaseMS > protocol.ExecutionLeaseDuration.Milliseconds() {
			return 0, delegation.ErrDenied
		}
		return time.Duration(reply.LeaseMS) * time.Millisecond, nil
	case <-ctx.Done():
		return 0, ctx.Err()
	case <-link.ctx.Done():
		return 0, link.ctx.Err()
	}
}
