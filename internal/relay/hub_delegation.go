package relay

import (
	"errors"
	"time"

	"github.com/xallor-ai/xallor-remote/internal/delegation"
	"github.com/xallor-ai/xallor-remote/internal/protocol"
)

func (h *Hub) renewExecution(from Sender, role string, message protocol.Message) {
	reply := protocol.Message{Type: protocol.TypeExecutionLease, RequestID: message.RequestID, Code: protocol.Unauthorized}
	if len(reply.RequestID) > 128 {
		reply.RequestID = ""
	}
	if role != protocol.RoleDevice || message.RequestID == "" || len(message.RequestID) > 128 || len(message.Delegation) > protocol.MaxDelegationBytes || h.executionAuthority == nil {
		_ = from.Enqueue(reply)
		return
	}
	h.mu.Lock()
	var id, uid string
	for candidate, device := range h.devices {
		if device.conn == from && device.ready {
			id, uid = candidate, device.record.UID
			break
		}
	}
	h.mu.Unlock()
	if id == "" {
		_ = from.Enqueue(reply)
		return
	}
	ctx, release, err := h.beginIdentity(from, id)
	if err != nil {
		reply.Code = protocol.RelayError
		_ = from.Enqueue(reply)
		return
	}
	defer release()
	ttl, err := h.executionAuthority.Renew(ctx, id, uid, message.Delegation)
	if err == nil {
		reply.Code = ""
		reply.LeaseMS = int64(ttl / time.Millisecond)
	} else if !errors.Is(err, delegation.ErrDenied) {
		reply.Code = protocol.RelayError
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	// Replies on replaced connections must not refresh the replacement Runtime.
	if device := h.devices[id]; device != nil && device.conn == from && device.record.UID == uid && ctx.Err() == nil {
		_ = from.Enqueue(reply)
	}
}
