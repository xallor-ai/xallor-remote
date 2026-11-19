package relay

import (
	"context"
	"sync"
	"time"

	"github.com/xallor-ai/xallor-remote/internal/access"
	"github.com/xallor-ai/xallor-remote/internal/accesscode"
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
	sessionTarget     accesscode.TargetBinding
	sessionConnection Sender
	conn              Sender
	record            access.Device
	online            bool
	ready             bool
}

type clientSlot struct {
	targetID        string
	access          access.Binding
	identity        principal.Identity
	authorization   access.Authorization
	session         *accesscode.SessionAuthorization
	sessionRequired bool
}

type pendingSession struct {
	target        accesscode.TargetBinding
	device        *deviceSlot
	connection    Sender
	request       accesscode.SessionRequest
	digest        string
	client        Sender
	targetID      string
	reservationID string
}

type inflight struct {
	requestID string
	deviceID  string
	client    Sender
	outBytes  int
	trunc     bool
	binding   clientSlot
	operation string
	cancelled bool
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
	accessCodes        accesscode.Store
	pendingSessions    map[string]*pendingSession
}

// NewHub receives independently composed services. Nil account/access services
// disable their operations; a store's incidental methods never enable them.
func NewHub(store DeviceStore, accounts principal.Authenticator, accessService DeviceAccess, executionAuthority ExecutionAuthority, transport *admission.Service, codeStores ...accesscode.Store) *Hub {
	if transport == nil {
		panic("relay: transport admission is required")
	}
	h := &Hub{
		devices:            map[string]*deviceSlot{},
		access:             map[string]access.Binding{},
		clients:            map[Sender]clientSlot{},
		inflight:           map[string]*inflight{},
		requests:           map[requestKey]string{},
		pendingSessions:    map[string]*pendingSession{},
		store:              store,
		accounts:           accounts,
		accessService:      accessService,
		executionAuthority: executionAuthority,
		admission:          transport,
	}
	if len(codeStores) > 0 {
		h.accessCodes = codeStores[0]
		if setter, ok := executionAuthority.(interface{ SetSessionStore(accesscode.Store) }); ok {
			setter.SetSessionStore(h.accessCodes)
		}
	}
	return h
}

func (h *Hub) Handle(from Sender, role string, targetID string, msg protocol.Message) {
	if role == protocol.RoleClient && from.Context().Err() != nil {
		return
	}
	switch msg.Type {
	case protocol.TypeExecutionRenewRoute:
		h.renewExecutionRoute(from, role, msg)
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
	case protocol.TypeSessionRequest:
		if role != protocol.RoleClient {
			_ = from.Enqueue(protocol.Message{Type: protocol.TypeSessionResult, RequestID: msg.RequestID, SessionID: msg.SessionID, Status: protocol.SessionRejectedState, Code: protocol.Unauthorized})
			return
		}
		h.handleSessionRequest(from, targetID, msg)
	case protocol.TypeSessionResumeRequest:
		if role != protocol.RoleClient {
			_ = from.Enqueue(protocol.Message{Type: protocol.TypeSessionResumeResult, RequestID: msg.RequestID, SessionID: msg.SessionID, Status: protocol.SessionRejectedState, Code: protocol.Unauthorized})
			return
		}
		h.handleSessionResumeRequest(from, targetID, msg)
	case protocol.TypeSessionApprovalResult:
		if role != protocol.RoleDevice {
			_ = from.Enqueue(protocol.Message{Type: protocol.TypeError, Code: protocol.Unauthorized})
			return
		}
		h.handleSessionApproval(from, targetID, msg)
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
	var pending []*pendingSession
	var revoked []*accesscode.SessionAuthorization
	var retired accesscode.TargetBinding
	h.mu.Lock()
	if role == protocol.RoleDevice {
		if d := h.devices[deviceOrTarget]; d != nil && (d.conn == from || d.sessionConnection == from) {
			retired = h.retireDeviceLocked(deviceOrTarget, d)
		}
	} else {
		if client, ok := h.clients[from]; ok && client.session != nil {
			copy := *client.session
			revoked = append(revoked, &copy)
		}
		for id, session := range h.pendingSessions {
			if session.client == from {
				delete(h.pendingSessions, id)
				pending = append(pending, session)
			}
		}
		h.cancelClientLocked(from)
		delete(h.clients, from)
	}
	h.mu.Unlock()
	_ = h.retireSessionTarget(retired)
	for _, session := range pending {
		ctx := context.Background()
		if role == protocol.RoleDevice {
			_ = h.markSessionUnknown(ctx, session.reservationID)
		} else {
			_ = h.releaseSessionReservation(ctx, session.reservationID, session.request.SessionID)
		}
	}
	if role == protocol.RoleClient && h.accessCodes != nil {
		kept := revoked[:0]
		for _, session := range revoked {
			if err := h.accessCodes.ReadAccessCode(context.Background(), func(reader accesscode.Reader) error {
				resume, err := reader.ActiveResumeForSession(context.Background(), session.ID)
				if err != nil || !time.Now().UTC().Before(resume.ExpiresAt) {
					return accesscode.ErrResumeExpired
				}
				return nil
			}); err == nil {
				continue
			}
			kept = append(kept, session)
		}
		revoked = kept
	}
	h.revokeSessions(revoked)
}

func (h *Hub) DeviceOnline(id string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	d := h.devices[id]
	return d != nil && d.online
}
