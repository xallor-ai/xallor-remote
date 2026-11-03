package relay

import (
	"context"
	"sync"

	"github.com/xallor-ai/xallor-remote/internal/access"
	"github.com/xallor-ai/xallor-remote/internal/admission"
	"github.com/xallor-ai/xallor-remote/internal/principal"
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
	ready  bool
}

type clientSlot struct {
	targetID      string
	access        access.Binding
	identity      principal.Identity
	authorization access.Authorization
}

type inflight struct {
	requestID string
	deviceID  string
	client    Sender
	outBytes  int
	trunc     bool
	identity  principal.Identity
	lease     *admission.Lease
}

// Hub is the in-memory router. Identity persistence is owned by DeviceStore.
type Hub struct {
	mu                 sync.Mutex
	devices            map[string]*deviceSlot
	access             map[string]access.Binding
	clients            map[Sender]clientSlot
	inflight           map[string]*inflight
	requests           map[requestKey]string
	store              DeviceStore
	accounts           principal.Authenticator
	accessService      DeviceAccess
	executionAuthority ExecutionAuthority
	admission          *admission.Service
	identities         identityGates
}

// NewHub receives independently composed services. Nil account/access services
// disable their operations; a store's incidental methods never enable them.
func NewHub(store DeviceStore, accounts principal.Authenticator, accessService DeviceAccess, executionAuthority ExecutionAuthority, transport *admission.Service) *Hub {
	if transport == nil {
		panic("relay: transport admission is required")
	}
	return &Hub{
		devices:            map[string]*deviceSlot{},
		access:             map[string]access.Binding{},
		clients:            map[Sender]clientSlot{},
		inflight:           map[string]*inflight{},
		requests:           map[requestKey]string{},
		store:              store,
		accounts:           accounts,
		accessService:      accessService,
		executionAuthority: executionAuthority,
		admission:          transport,
	}
}

func (h *Hub) Handle(from Sender, role string, targetID string, msg protocol.Message) {
	if role == protocol.RoleClient && from.Context().Err() != nil {
		return
	}
	switch msg.Type {
	case protocol.TypeExecutionRenew:
		h.renewExecution(from, role, msg)
	case protocol.TypeAccessRequest:
		if role != protocol.RoleDevice {
			if len(msg.RequestID) > 128 {
				msg.RequestID = ""
			}
			_ = from.Enqueue(protocol.Message{Type: protocol.TypeAccessResult, RequestID: msg.RequestID, Code: protocol.Unauthorized})
			return
		}
		h.handleAccess(from, msg)
	case protocol.TypeHeartbeat:
		return
	case protocol.TypeDeviceSettings, protocol.TypeRevoke:
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

func (h *Hub) DeviceOnline(id string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	d := h.devices[id]
	return d != nil && d.online
}
