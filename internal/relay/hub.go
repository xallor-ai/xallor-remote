package relay

import (
	"context"
	"sync"

	"github.com/xallor-ai/xallor-remote/internal/access"
	"github.com/xallor-ai/xallor-remote/internal/protocol"
)

// Sender admits messages to a bounded queue. Neither method may perform I/O or
// wait for a peer; the connection owner drains the queue independently.
type Sender interface {
	Context() context.Context
	Enqueue(protocol.Message) error
	Close()
}

type deviceSlot struct {
	conn   Sender
	record access.Device
	online bool
}

type clientSlot struct {
	targetID string
	access   access.Binding
}

type inflight struct {
	deviceID string
	client   Sender
	outBytes int
	trunc    bool
}

// Hub is the in-memory router. Identity persistence is owned by DeviceStore.
type Hub struct {
	mu         sync.Mutex
	devices    map[string]*deviceSlot
	access     map[string]access.Binding
	clients    map[Sender]clientSlot
	inflight   map[string]*inflight
	store      DeviceStore
	identities identityGates
}

func NewHub(store DeviceStore) *Hub {
	return &Hub{
		devices:  map[string]*deviceSlot{},
		access:   map[string]access.Binding{},
		clients:  map[Sender]clientSlot{},
		inflight: map[string]*inflight{},
		store:    store,
	}
}

func (h *Hub) Handle(from Sender, role string, targetID string, msg protocol.Message) {
	if role == protocol.RoleClient && from.Context().Err() != nil {
		return
	}
	switch msg.Type {
	case protocol.TypeHeartbeat:
		return
	case protocol.TypeGrantRotate, protocol.TypeRevoke, protocol.TypeInboundSet:
		if role != protocol.RoleDevice {
			_ = from.Enqueue(protocol.Message{Type: protocol.TypeError, Code: protocol.Unauthorized})
			return
		}
		h.handleAuth(from, msg)
	case protocol.TypeInvoke:
		if role != protocol.RoleClient {
			_ = from.Enqueue(protocol.Nack(msg.ExecID, protocol.Unauthorized))
			return
		}
		h.handleInvoke(from, targetID, msg)
	case protocol.TypeStdout, protocol.TypeStderr, protocol.TypeExit, protocol.TypeError, protocol.TypeInvokeNack:
		if role != protocol.RoleDevice {
			return
		}
		h.forwardToClient(from, msg)
	default:
		return
	}
}

func (h *Hub) Drop(from Sender, role, deviceOrTarget string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if role == protocol.RoleDevice {
		if d := h.devices[deviceOrTarget]; d != nil && d.conn == from {
			h.failDeviceLocked(deviceOrTarget, protocol.DeviceOffline)
			d.online = false
			d.conn = nil
		}
		return
	}
	h.cancelClientLocked(from)
	delete(h.clients, from)
}

func (h *Hub) handleInvoke(from Sender, targetID string, msg protocol.Message) {
	if msg.Op == protocol.OpCancel {
		h.handleCancel(from, msg.ExecID)
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
	if _, dup := h.inflight[msg.ExecID]; dup {
		_ = from.Enqueue(protocol.Nack(msg.ExecID, protocol.AgentError))
		return
	}
	dev := h.devices[targetID]
	if dev == nil || !dev.online || dev.conn == nil || dev.record.UID != binding.access.DeviceUID {
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
	n := 0
	for _, inf := range h.inflight {
		if inf.deviceID == targetID {
			n++
		}
	}
	if n >= protocol.MaxConcurrentExec {
		_ = from.Enqueue(protocol.Nack(msg.ExecID, protocol.QuotaExceeded))
		return
	}
	// Recheck at admission: disconnect can race with waiting for the routing lock.
	if from.Context().Err() != nil {
		return
	}
	h.inflight[msg.ExecID] = &inflight{deviceID: targetID, client: from}
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
	inf := h.inflight[execID]
	if inf == nil || inf.client != from {
		_ = from.Enqueue(protocol.Err(execID, protocol.UnknownExec))
		return
	}
	if d := h.devices[inf.deviceID]; d != nil && d.conn != nil {
		_ = d.conn.Enqueue(protocol.Message{Type: protocol.TypeInvoke, ExecID: execID, Op: protocol.OpCancel})
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
		delete(h.inflight, msg.ExecID)
	}
}

func (h *Hub) failDeviceLocked(id, code string) {
	for execID, inf := range h.inflight {
		if inf.deviceID == id {
			_ = inf.client.Enqueue(protocol.Err(execID, code))
			delete(h.inflight, execID)
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
		delete(h.inflight, execID)
	}
}

func (h *Hub) DeviceOnline(id string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	d := h.devices[id]
	return d != nil && d.online
}
