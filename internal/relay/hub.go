package relay

import (
	"sync"

	"github.com/xallor-ai/xallor-remote/internal/protocol"
)

// Sender is a live WSS connection. Writes must be serialized by the impl.
type Sender interface {
	Send(protocol.Message) error
	Close()
}

type deviceSlot struct {
	conn       Sender
	secretHash string
	inbound    bool
	online     bool
}

type clientSlot struct {
	targetID string
	grantHex string
}

type inflight struct {
	deviceID string
	client   Sender
	outBytes int
	trunc    bool
}

// Hub is the in-memory router. Identity persistence is owned by DeviceStore.
type Hub struct {
	mu       sync.Mutex
	devices  map[string]*deviceSlot
	grants   map[string]string // device_id -> grant hash
	clients  map[Sender]clientSlot
	inflight map[string]*inflight
	store    DeviceStore
}

func NewHub(store DeviceStore) *Hub {
	return &Hub{
		devices:  map[string]*deviceSlot{},
		grants:   map[string]string{},
		clients:  map[Sender]clientSlot{},
		inflight: map[string]*inflight{},
		store:    store,
	}
}

func (h *Hub) Handle(from Sender, role string, targetID string, msg protocol.Message) {
	switch msg.Type {
	case protocol.TypeHeartbeat:
		return
	case protocol.TypeGrantRotate, protocol.TypeRevoke, protocol.TypeInboundSet:
		if role != protocol.RoleDevice {
			_ = from.Send(protocol.Message{Type: protocol.TypeError, Code: protocol.Unauthorized})
			return
		}
		h.handleAuth(from, msg)
	case protocol.TypeInvoke:
		if role != protocol.RoleClient {
			_ = from.Send(protocol.Nack(msg.ExecID, protocol.Unauthorized))
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
	if binding, ok := h.clients[from]; !ok || binding.targetID != targetID || binding.grantHex != h.grants[targetID] {
		_ = from.Send(protocol.Nack(msg.ExecID, protocol.Unauthorized))
		return
	}
	if msg.ExecID == "" {
		_ = from.Send(protocol.Nack(msg.ExecID, protocol.AgentError))
		h.record(targetID, msg.Op, msg.ExecID, "nack", protocol.AgentError, msg.Payload)
		return
	}
	if _, dup := h.inflight[msg.ExecID]; dup {
		_ = from.Send(protocol.Nack(msg.ExecID, protocol.AgentError))
		h.record(targetID, msg.Op, msg.ExecID, "nack", protocol.AgentError, msg.Payload)
		return
	}
	dev := h.devices[targetID]
	if dev == nil || !dev.online || dev.conn == nil {
		code := protocol.UnknownDevice
		if h.store != nil {
			if _, err := h.store.LoadDevice(targetID); err == nil {
				code = protocol.DeviceOffline
			} else {
				code = identityErrorCode(err)
			}
		} else if dev != nil {
			code = protocol.DeviceOffline
		}
		_ = from.Send(protocol.Nack(msg.ExecID, code))
		h.record(targetID, msg.Op, msg.ExecID, "nack", code, msg.Payload)
		return
	}
	if !dev.inbound {
		_ = from.Send(protocol.Nack(msg.ExecID, protocol.InboundDisabled))
		h.record(targetID, msg.Op, msg.ExecID, "nack", protocol.InboundDisabled, msg.Payload)
		return
	}
	n := 0
	for _, inf := range h.inflight {
		if inf.deviceID == targetID {
			n++
		}
	}
	if n >= protocol.MaxConcurrentExec {
		_ = from.Send(protocol.Nack(msg.ExecID, protocol.QuotaExceeded))
		h.record(targetID, msg.Op, msg.ExecID, "nack", protocol.QuotaExceeded, msg.Payload)
		return
	}
	h.inflight[msg.ExecID] = &inflight{deviceID: targetID, client: from}
	h.record(targetID, msg.Op, msg.ExecID, "accept", "", msg.Payload)
	_ = dev.conn.Send(msg)
}

func (h *Hub) handleCancel(from Sender, execID string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	inf := h.inflight[execID]
	if inf == nil || inf.client != from {
		_ = from.Send(protocol.Err(execID, protocol.UnknownExec))
		return
	}
	if d := h.devices[inf.deviceID]; d != nil && d.conn != nil {
		_ = d.conn.Send(protocol.Message{Type: protocol.TypeInvoke, ExecID: execID, Op: protocol.OpCancel})
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
		inf.outBytes += len(msg.Data)
		if inf.outBytes > protocol.MaxExecOutputBytes || len(msg.Data) > protocol.MaxFrameBytes {
			inf.trunc = true
			return
		}
	}
	out := msg
	if inf.trunc && msg.Type == protocol.TypeExit {
		out.Truncated = protocol.Bool(true)
	}
	_ = inf.client.Send(out)
	if msg.Type == protocol.TypeExit || msg.Type == protocol.TypeError || msg.Type == protocol.TypeInvokeNack {
		decision := "exit"
		code := msg.Status
		if msg.Type != protocol.TypeExit {
			decision = "error"
			code = msg.Code
		}
		h.record(inf.deviceID, "", msg.ExecID, decision, code, nil)
		delete(h.inflight, msg.ExecID)
	}
}

func (h *Hub) failDeviceLocked(id, code string) {
	for execID, inf := range h.inflight {
		if inf.deviceID == id {
			_ = inf.client.Send(protocol.Err(execID, code))
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
			_ = d.conn.Send(protocol.Message{Type: protocol.TypeInvoke, ExecID: execID, Op: protocol.OpCancel})
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
