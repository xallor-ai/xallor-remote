package relay

import (
	"errors"

	"github.com/xallor-ai/xallor-remote/internal/admission"
	"github.com/xallor-ai/xallor-remote/internal/protocol"
)

// Caller correlation is local to a connection. Device-side routing uses a fresh
// Relay-generated ID, so a delayed event cannot attach to a replacement request.
type requestKey struct {
	client Sender
	id     string
}

func (h *Hub) finishInvocationLocked(routeID string) {
	if task := h.inflight[routeID]; task != nil {
		delete(h.requests, requestKey{client: task.client, id: task.requestID})
		task.lease.Release()
		delete(h.inflight, routeID)
	}
}

func (h *Hub) handleInvoke(from Sender, targetID string, msg protocol.Message) {
	if msg.Op == protocol.OpCancel {
		h.handleCancel(from, msg.ExecID)
		return
	}
	if code := h.verifyClientAccess(from, msg.Op); code != "" {
		_ = from.Enqueue(protocol.Nack(msg.ExecID, code))
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	binding, ok := h.clients[from]
	if !ok || binding.targetID != targetID || !binding.access.Matches(h.access[targetID]) {
		_ = from.Enqueue(protocol.Nack(msg.ExecID, protocol.Unauthorized))
		return
	}
	if msg.ExecID == "" {
		_ = from.Enqueue(protocol.Nack(msg.ExecID, protocol.AgentError))
		return
	}
	key := requestKey{client: from, id: msg.ExecID}
	if _, dup := h.requests[key]; dup {
		_ = from.Enqueue(protocol.Nack(msg.ExecID, protocol.AgentError))
		return
	}
	dev := h.devices[targetID]
	if dev == nil || !dev.online || !dev.ready || dev.conn == nil || dev.record.UID != binding.access.DeviceUID {
		// A live authenticated binding already proves that the identity exists.
		// Revocation removes its grant before routing can reach this branch.
		code := protocol.DeviceOffline
		_ = from.Enqueue(protocol.Nack(msg.ExecID, code))
		return
	}
	if !dev.record.Inbound {
		_ = from.Enqueue(protocol.Nack(msg.ExecID, protocol.InboundDisabled))
		return
	}
	lease, err := h.admission.Acquire(admission.Request{DeviceUID: binding.access.DeviceUID})
	if err != nil {
		code := protocol.RelayError
		if errors.Is(err, admission.ErrLimit) {
			code = protocol.QuotaExceeded
		}
		_ = from.Enqueue(protocol.Nack(msg.ExecID, code))
		return
	}
	defer func() {
		if lease != nil {
			lease.Release()
		}
	}()
	// Recheck at admission: disconnect can race with waiting for the routing lock.
	if from.Context().Err() != nil {
		return
	}
	routeID, err := protocol.NewExecID()
	if err != nil {
		_ = from.Enqueue(protocol.Nack(msg.ExecID, protocol.RelayError))
		return
	}
	if _, exists := h.inflight[routeID]; exists {
		_ = from.Enqueue(protocol.Nack(msg.ExecID, protocol.RelayError))
		return
	}
	var proof string
	if msg.Op == protocol.OpHandoffSubmit {
		if h.executionAuthority == nil {
			_ = from.Enqueue(protocol.Nack(msg.ExecID, protocol.Unauthorized))
			return
		}
		proof, err = h.executionAuthority.Issue(targetID, binding.access, binding.identity, binding.authorization)
		if err != nil {
			_ = from.Enqueue(protocol.Nack(msg.ExecID, protocol.RelayError))
			return
		}
	}
	h.requests[key] = routeID
	h.inflight[routeID] = &inflight{requestID: msg.ExecID, deviceID: targetID, client: from, binding: binding, operation: msg.Op, lease: lease}
	lease = nil
	// Reconstruct the invoke: credentials, forged scopes and unrelated envelope
	// fields never reach the device. Cancel is separately bound to its connection.
	scope := h.invocationScope(binding)
	msg = protocol.Message{Type: protocol.TypeInvoke, ExecID: routeID, Op: msg.Op, Payload: msg.Payload, Scope: &scope, Delegation: proof}
	if err := dev.conn.Enqueue(msg); err != nil {
		h.failDeviceLocked(targetID, protocol.DeviceOffline)
		dev.conn.Close()
		dev.online = false
		dev.conn = nil
	}
}

func (h *Hub) handleCancel(from Sender, execID string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	routeID := h.requests[requestKey{client: from, id: execID}]
	inf := h.inflight[routeID]
	if inf == nil || inf.client != from {
		_ = from.Enqueue(protocol.Err(execID, protocol.UnknownExec))
		return
	}
	inf.cancelled = true
	if d := h.devices[inf.deviceID]; d != nil && d.conn != nil {
		_ = d.conn.Enqueue(protocol.Message{Type: protocol.TypeInvoke, ExecID: routeID, Op: protocol.OpCancel})
	}
}

func (h *Hub) forwardToClient(from Sender, msg protocol.Message) {
	h.mu.Lock()
	defer h.mu.Unlock()
	inf := h.inflight[msg.ExecID]
	if inf == nil {
		return
	}
	if device := h.devices[inf.deviceID]; device == nil || device.conn != from {
		return
	}
	if msg.Type == protocol.TypeStdout || msg.Type == protocol.TypeStderr {
		if len(msg.Data) > protocol.MaxFrameBytes {
			inf.trunc = true
			return
		}
		if len(msg.Data) > protocol.MaxExecOutputBytes-inf.outBytes {
			inf.outBytes = protocol.MaxExecOutputBytes
			inf.trunc = true
			return
		}
		inf.outBytes += len(msg.Data)
	}
	out := msg
	out.ExecID = inf.requestID
	if inf.trunc && msg.Type == protocol.TypeExit {
		out.Truncated = protocol.Bool(true)
	}
	if err := inf.client.Enqueue(out); err != nil {
		if err == errOutputDropped {
			inf.trunc = true
			return
		}
		h.cancelClientLocked(inf.client)
		delete(h.clients, inf.client)
		inf.client.Close()
		return
	}
	if msg.Type == protocol.TypeExit || msg.Type == protocol.TypeError || msg.Type == protocol.TypeInvokeNack {
		h.finishInvocationLocked(msg.ExecID)
	}
}

func (h *Hub) failDeviceLocked(id, code string) {
	for execID, inf := range h.inflight {
		if inf.deviceID == id {
			_ = inf.client.Enqueue(protocol.Err(inf.requestID, code))
			h.finishInvocationLocked(execID)
		}
	}
}

func (h *Hub) cancelClientLocked(from Sender) {
	for execID, inf := range h.inflight {
		if inf.client != from {
			continue
		}
		if d := h.devices[inf.deviceID]; d != nil && d.conn != nil {
			_ = d.conn.Enqueue(protocol.Message{Type: protocol.TypeInvoke, ExecID: execID, Op: protocol.OpCancel})
		}
		h.finishInvocationLocked(execID)
	}
}
