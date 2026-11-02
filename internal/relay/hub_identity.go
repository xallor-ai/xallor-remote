package relay

import (
	"context"
	"errors"

	"github.com/xallor-ai/xallor-remote/internal/access"
	"github.com/xallor-ai/xallor-remote/internal/principal"
	"github.com/xallor-ai/xallor-remote/internal/protocol"
)

func (h *Hub) beginIdentity(conn Sender, id string) (context.Context, func(), error) {
	ctx, cancel := context.WithTimeout(conn.Context(), protocol.RelayIdentityTimeout)
	release, err := h.identities.acquire(ctx, id)
	if err != nil {
		cancel()
		return nil, nil, err
	}
	return ctx, func() { release(); cancel() }, nil
}

func (h *Hub) RegisterDevice(id, secret string, inbound bool, conn Sender) string {
	ctx, release, err := h.beginIdentity(conn, id)
	if err != nil {
		return identityErrorCode(err)
	}
	defer release()
	hash := protocol.SHA256Hex(secret)
	var record access.Device
	if h.store != nil {
		record, err = h.store.RegisterDevice(ctx, id, hash, inbound)
		if err != nil {
			return identityErrorCode(err)
		}
	} else {
		record, err = access.NewDevice(hash, inbound)
		if err != nil {
			return protocol.RelayError
		}
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if ctx.Err() != nil {
		return protocol.RelayError
	}
	if h.store == nil {
		if old := h.devices[id]; old != nil {
			if old.record.SecretHash != hash {
				return protocol.Unauthorized
			}
			record = old.record
		}
	}
	// Queue the greeting before publishing a routable connection, so an invoke
	// racing with registration can never be the peer's first received frame.
	if err := conn.Enqueue(protocol.Message{Type: protocol.TypeHelloOK, Role: protocol.RoleDevice}); err != nil {
		return protocol.RelayError
	}
	if old := h.devices[id]; old != nil && old.conn != nil && old.conn != conn {
		h.failDeviceLocked(id, protocol.DeviceOffline)
		old.conn.Close()
	}
	h.access[id] = record.Binding()
	h.devices[id] = &deviceSlot{conn: conn, record: record, online: true}
	return ""
}

func (h *Hub) RegisterClient(targetID, grant, accountToken string, conn Sender) string {
	ctx, release, err := h.beginIdentity(conn, targetID)
	if err != nil {
		return identityErrorCode(err)
	}
	defer release()
	var identity principal.Identity
	if accountToken != "" {
		auth, ok := h.store.(principal.Authenticator)
		if !ok {
			return protocol.Unauthorized
		}
		identity, err = auth.Authenticate(ctx, accountToken)
		if err != nil {
			return principalErrorCode(err)
		}
	}
	want := protocol.SHA256Hex(grant)
	var record access.Device
	if h.store != nil {
		record, err = h.store.LoadDevice(ctx, targetID)
		if err != nil {
			return identityErrorCode(err)
		}
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if ctx.Err() != nil {
		return protocol.RelayError
	}
	if h.store == nil {
		if h.devices[targetID] == nil {
			return protocol.UnknownDevice
		}
		record = h.devices[targetID].record
	}
	if record.GrantHash == "" || record.GrantHash != want || record.UID == "" || record.GrantID == "" {
		return protocol.Unauthorized
	}
	if err := conn.Enqueue(protocol.Message{Type: protocol.TypeHelloOK, Role: protocol.RoleClient}); err != nil {
		return protocol.RelayError
	}
	h.access[targetID] = record.Binding()
	h.clients[conn] = clientSlot{targetID: targetID, access: record.Binding(), identity: identity}
	return ""
}

func identityErrorCode(err error) string {
	switch {
	case errors.Is(err, ErrDeviceNotFound):
		return protocol.UnknownDevice
	case errors.Is(err, ErrDeviceIdentity):
		return protocol.Unauthorized
	default:
		return protocol.RelayError
	}
}
