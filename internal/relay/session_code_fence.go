package relay

import (
	"github.com/xallor-ai/xallor-remote/internal/accesscode"
	"github.com/xallor-ai/xallor-remote/internal/protocol"
)

func consentUsesCode(session *accesscode.SessionAuthorization, deviceUID, codeID string, generation uint64) bool {
	return session != nil && session.Target.DeviceUID == deviceUID && session.CodeID == codeID && session.Generation == generation
}

// Called under h.mu after durable code revocation/rotation. A route keeps its
// original consent even when its controller adopts another session. Notify only
// the same target incarnation, and retain streams/leases until actual terminal.
func (h *Hub) invalidateCodeSessionsLocked(deviceUID, codeID string, generation uint64) {
	for execID, route := range h.inflight {
		if route.cancelled || route.binding.access.DeviceUID != deviceUID || !consentUsesCode(route.binding.session, deviceUID, codeID, generation) {
			continue
		}
		route.cancelled = true
		device := h.devices[route.deviceID]
		if device != nil && device.conn != nil && device.ready && device.record.UID == deviceUID &&
			route.binding.session.Target.Valid() && device.sessionTarget == route.binding.session.Target {
			// Queue failure cannot authorize another renewal or prove cleanup.
			// The durable denial/lease expiry remains the bounded fallback.
			_ = device.conn.Enqueue(protocol.Message{Type: protocol.TypeInvoke, Op: protocol.OpCancel, ExecID: execID, Code: protocol.Unauthorized})
		}
	}
	// Current consent is detached separately; do not overwrite a newer session
	// merely because one of this connection's old executions was revoked.
	for conn, client := range h.clients {
		if client.access.DeviceUID != deviceUID || !consentUsesCode(client.session, deviceUID, codeID, generation) {
			continue
		}
		client.session, client.sessionRequired = nil, true
		h.clients[conn] = client
	}
}
