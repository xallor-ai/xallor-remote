package relay

import (
	"encoding/json"
	"errors"

	"github.com/xallor-ai/xallor-remote/internal/access"
	"github.com/xallor-ai/xallor-remote/internal/principal"
	"github.com/xallor-ai/xallor-remote/internal/protocol"
)

func (h *Hub) handleAccess(from Sender, msg protocol.Message) {
	requestID := msg.RequestID
	if len(requestID) > 128 {
		requestID = ""
	}
	reply := func(code string, state any) {
		var payload json.RawMessage
		if state != nil {
			payload, _ = json.Marshal(state)
		}
		_ = from.Enqueue(protocol.Message{Type: protocol.TypeAccessResult, RequestID: requestID, Code: code, Payload: payload})
	}
	var req access.Request
	if msg.RequestID == "" || len(msg.RequestID) > 128 || len(msg.Payload) > 16*1024 || json.Unmarshal(msg.Payload, &req) != nil || req.Validate() != nil {
		reply(protocol.AgentError, nil)
		return
	}
	if h.accessService == nil || h.accounts == nil {
		reply(protocol.Unauthorized, nil)
		return
	}
	h.mu.Lock()
	var id string
	for candidate, device := range h.devices {
		if device.conn == from {
			id = candidate
			break
		}
	}
	h.mu.Unlock()
	if id == "" {
		reply(protocol.Unauthorized, nil)
		return
	}
	ctx, release, err := h.beginIdentity(from, id)
	if err != nil {
		reply(accessErrorCode(err), nil)
		return
	}
	defer release()
	h.mu.Lock()
	device := h.devices[id]
	if device == nil || device.conn != from {
		h.mu.Unlock()
		reply(protocol.Unauthorized, nil)
		return
	}
	expected := device.record
	h.mu.Unlock()
	actor, err := h.accounts.Authenticate(ctx, msg.AccountToken)
	if err != nil {
		reply(accessErrorCode(err), nil)
		return
	}
	mutation, err := h.accessService.Manage(ctx, id, expected, actor, req)
	if err != nil {
		code := accessErrorCode(err)
		if code == protocol.RelayError && req.Action != "status" {
			// COMMIT may have succeeded despite a driver error. Stop the old route
			// before reporting failure; a fresh device handshake reloads authority.
			h.mu.Lock()
			h.stopDeviceRoutingLocked(from)
			reply(code, nil)
			from.Close()
			h.mu.Unlock()
		} else {
			reply(code, nil)
		}
		return
	}
	h.mu.Lock()
	// A committed result survives disconnect, but cannot overwrite a replacement.
	if h.devices[id] == device {
		if expected.OwnershipID != mutation.Device.OwnershipID {
			h.invalidateOwnershipLocked(id)
		}
		device.record = mutation.Device
		h.access[id] = mutation.Device.Binding()
	}
	// Publish the authoritative context in FIFO order before another invoke can
	// enter this device's outbox. A lost confirmation is reconciled at reconnect.
	payload, _ := json.Marshal(mutation.State)
	if err := from.Enqueue(protocol.Message{Type: protocol.TypeAccessResult, RequestID: requestID, Payload: payload, DeviceContext: h.deviceContext(mutation.Device)}); err != nil {
		h.stopDeviceRoutingLocked(from)
		from.Close()
	}
	h.mu.Unlock()
}

// Foreground invocations lose their channel on ownership change. The device
// context also cancels old handoff workers; neither signal proves cleanup.
func (h *Hub) invalidateOwnershipLocked(id string) {
	device := h.devices[id]
	for execID, task := range h.inflight {
		if task.deviceID != id {
			continue
		}
		if device != nil && device.conn != nil {
			_ = device.conn.Enqueue(protocol.Message{Type: protocol.TypeInvoke, Op: protocol.OpCancel, ExecID: execID})
		}
		_ = task.client.Enqueue(protocol.Err(task.requestID, protocol.Unauthorized))
		h.finishInvocationLocked(execID)
	}
	for connection, client := range h.clients {
		if client.targetID == id {
			delete(h.clients, connection)
			connection.Close()
		}
	}
}

func accessErrorCode(err error) string {
	switch {
	case errors.Is(err, access.ErrDenied), errors.Is(err, principal.ErrUnauthorized), errors.Is(err, access.ErrDeviceIdentity):
		return protocol.Unauthorized
	case errors.Is(err, access.ErrInvalid):
		return protocol.AgentError
	case errors.Is(err, access.ErrLimit):
		return protocol.QuotaExceeded
	case errors.Is(err, access.ErrDeviceNotFound):
		return protocol.UnknownDevice
	default:
		return protocol.RelayError
	}
}
