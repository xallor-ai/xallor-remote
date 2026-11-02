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
	store, ok := h.store.(access.Store)
	auth, authOK := h.store.(principal.Authenticator)
	if !ok || !authOK {
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
	actor, err := auth.Authenticate(ctx, msg.AccountToken)
	if err != nil {
		reply(accessErrorCode(err), nil)
		return
	}
	mutation, err := store.ManageAccess(ctx, id, expected, actor, req)
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
	h.mu.Unlock()
	reply("", mutation.State)
}

// Foreground invocations lose their channel on ownership change. Cancel is a
// best-effort stop request; this does not claim background handoff workers ended.
func (h *Hub) invalidateOwnershipLocked(id string) {
	device := h.devices[id]
	for execID, task := range h.inflight {
		if task.deviceID != id {
			continue
		}
		if device != nil && device.conn != nil {
			_ = device.conn.Enqueue(protocol.Message{Type: protocol.TypeInvoke, Op: protocol.OpCancel, ExecID: execID})
		}
		_ = task.client.Enqueue(protocol.Err(execID, protocol.Unauthorized))
		delete(h.inflight, execID)
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
	case errors.Is(err, access.ErrDenied), errors.Is(err, principal.ErrUnauthorized), errors.Is(err, ErrDeviceIdentity):
		return protocol.Unauthorized
	case errors.Is(err, access.ErrInvalid):
		return protocol.AgentError
	case errors.Is(err, access.ErrLimit):
		return protocol.QuotaExceeded
	case errors.Is(err, ErrDeviceNotFound):
		return protocol.UnknownDevice
	default:
		return protocol.RelayError
	}
}
