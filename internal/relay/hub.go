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
	conn     Sender
	targetID string
	grantHex string
}

type inflight struct {
	deviceID string
	client   Sender
	outBytes int
	trunc    bool
}

// Hub is the in-memory router. Persistence of hashes is optional via Store.
type Hub struct {
	mu       sync.Mutex
	devices  map[string]*deviceSlot
	grants   map[string]string // device_id -> grant hash
	inflight map[string]*inflight
	store    Store
}

type Store interface {
	LoadDevice(id string) (secretHash string, grantHash string, inbound bool, ok bool)
	SaveDevice(id, secretHash, grantHash string, inbound bool)
	DeleteDevice(id string)
}

func NewHub(store Store) *Hub {
	return &Hub{
		devices:  map[string]*deviceSlot{},
		grants:   map[string]string{},
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
		h.forwardToClient(msg)
	default:
		return
	}
}

func (h *Hub) RegisterDevice(id, secret string, inbound bool, conn Sender) string {
	hash := protocol.SHA256Hex(secret)
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.store != nil {
		if sh, gh, inb, ok := h.store.LoadDevice(id); ok {
			if sh != hash {
				return protocol.Unauthorized
			}
			h.grants[id] = gh
			inbound = inb
		} else {
			h.store.SaveDevice(id, hash, "", inbound)
		}
	}
	if old, ok := h.devices[id]; ok && old.conn != nil && old.conn != conn {
		h.failDeviceLocked(id, protocol.DeviceOffline)
		old.conn.Close()
	}
	if gh, ok := h.grants[id]; !ok || gh == "" {
		if h.store != nil {
			if _, gh2, inb, ok := h.store.LoadDevice(id); ok {
				h.grants[id] = gh2
				inbound = inb
			}
		}
	}
	h.devices[id] = &deviceSlot{conn: conn, secretHash: hash, inbound: inbound, online: true}
	return ""
}

func (h *Hub) RegisterClient(targetID, grant string, conn Sender) string {
	want := protocol.SHA256Hex(grant)
	h.mu.Lock()
	defer h.mu.Unlock()
	gh := h.grants[targetID]
	if gh == "" && h.store != nil {
		if _, g, _, ok := h.store.LoadDevice(targetID); ok {
			gh = g
			h.grants[targetID] = g
		}
	}
	if gh == "" {
		if _, ok := h.devices[targetID]; !ok && (h.store == nil || func() bool {
			_, _, _, ok := h.store.LoadDevice(targetID)
			return !ok
		}()) {
			return protocol.UnknownDevice
		}
		return protocol.Unauthorized
	}
	if gh != want {
		return protocol.Unauthorized
	}
	return ""
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
}

func (h *Hub) handleInvoke(from Sender, targetID string, msg protocol.Message) {
	if msg.Op == protocol.OpCancel {
		h.handleCancel(from, msg.ExecID)
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
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
			if _, _, _, ok := h.store.LoadDevice(targetID); ok {
				code = protocol.DeviceOffline
			}
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

func (h *Hub) forwardToClient(msg protocol.Message) {
	h.mu.Lock()
	defer h.mu.Unlock()
	inf := h.inflight[msg.ExecID]
	if inf == nil {
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

func (h *Hub) handleAuth(from Sender, msg protocol.Message) {
	h.mu.Lock()
	defer h.mu.Unlock()
	var id string
	for did, d := range h.devices {
		if d.conn == from {
			id = did
			break
		}
	}
	if id == "" {
		return
	}
	switch msg.Type {
	case protocol.TypeGrantRotate:
		h.grants[id] = msg.NewGrantHash
		if d := h.devices[id]; d != nil && h.store != nil {
			h.store.SaveDevice(id, d.secretHash, msg.NewGrantHash, d.inbound)
		}
	case protocol.TypeInboundSet:
		if d := h.devices[id]; d != nil && msg.Inbound != nil {
			d.inbound = *msg.Inbound
			if h.store != nil {
				h.store.SaveDevice(id, d.secretHash, h.grants[id], d.inbound)
			}
		}
	case protocol.TypeRevoke:
		h.failDeviceLocked(id, protocol.DeviceOffline)
		if d := h.devices[id]; d != nil && d.conn != nil {
			d.conn.Close()
		}
		delete(h.devices, id)
		delete(h.grants, id)
		if h.store != nil {
			h.store.DeleteDevice(id)
		}
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
