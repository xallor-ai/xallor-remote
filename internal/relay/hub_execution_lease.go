package relay

import (
	"errors"
	"time"

	"github.com/xallor-ai/xallor-remote/internal/delegation"
	"github.com/xallor-ai/xallor-remote/internal/protocol"
)

// Foreground authority lives exactly as long as the original route. Unlike a
// background handoff, it needs no proof that outlives the submitting connection.
func (h *Hub) renewExecutionRoute(from Sender, role string, message protocol.Message) {
	reply := protocol.Message{Type: protocol.TypeExecutionLease, RequestID: message.RequestID, Code: protocol.Unauthorized}
	if len(reply.RequestID) > 128 {
		reply.RequestID = ""
	}
	if role != protocol.RoleDevice || message.RequestID == "" || len(message.RequestID) > 128 || message.ExecID == "" || len(message.ExecID) > 128 || message.Delegation != "" || h.executionAuthority == nil {
		_ = from.Enqueue(reply)
		return
	}
	h.mu.Lock()
	route := h.inflight[message.ExecID]
	if !h.liveExecutionRouteLocked(from, route) {
		h.mu.Unlock()
		_ = from.Enqueue(reply)
		return
	}
	binding := route.binding
	h.mu.Unlock()
	ctx, release, err := h.beginIdentity(from, route.deviceID)
	if err != nil {
		reply.Code = protocol.RelayError
		_ = from.Enqueue(reply)
		return
	}
	defer release()
	ttl, err := h.executionAuthority.RenewForeground(ctx, delegation.Invocation{
		DeviceID: route.deviceID, Binding: binding.access, Identity: binding.identity, Authorization: binding.authorization,
	})
	if err == nil {
		reply.Code = ""
		reply.LeaseMS = int64(ttl / time.Millisecond)
	} else if !errors.Is(err, delegation.ErrDenied) {
		reply.Code = protocol.RelayError
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	// Cancellation, route completion, and device/caller replacement win over a
	// delayed database success. Reusing the caller's request ID cannot revive it.
	if h.inflight[message.ExecID] != route || !h.liveExecutionRouteLocked(from, route) {
		reply.Code, reply.LeaseMS = protocol.Unauthorized, 0
	} else if ctx.Err() != nil {
		reply.Code, reply.LeaseMS = protocol.RelayError, 0
	}
	_ = from.Enqueue(reply)
}

func (h *Hub) liveExecutionRouteLocked(from Sender, route *inflight) bool {
	if route == nil || route.cancelled || route.operation != protocol.OpExec || route.client.Context().Err() != nil || from.Context().Err() != nil {
		return false
	}
	device := h.devices[route.deviceID]
	_, connected := h.clients[route.client]
	return connected && device != nil && device.ready && device.conn == from &&
		device.record.Inbound && route.binding.access.Matches(device.record.Binding()) &&
		route.binding.access.Matches(h.access[route.deviceID])
}
