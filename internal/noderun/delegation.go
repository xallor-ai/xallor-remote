package noderun

import (
	"context"
	"time"

	"github.com/xallor-ai/xallor-remote/internal/delegation"
	"github.com/xallor-ai/xallor-remote/internal/protocol"
)

func (d *Daemon) renewExecution(ctx context.Context, proof string) (time.Duration, error) {
	if proof == "" || len(proof) > protocol.MaxDelegationBytes {
		return 0, delegation.ErrDenied
	}
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
	if err = link.sendContext(ctx, protocol.Message{Type: protocol.TypeExecutionRenew, RequestID: key, Delegation: proof}); err != nil {
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
