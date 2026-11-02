package relay

import (
	"context"
	"errors"

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
	record := DeviceRecord{SecretHash: hash, Inbound: inbound}
	if h.store != nil {
		record, err = h.store.RegisterDevice(ctx, id, hash, inbound)
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
		record.GrantHash = h.grants[id]
		if old := h.devices[id]; old != nil {
			if old.secretHash != hash {
				return protocol.Unauthorized
			}
			record.Inbound = old.inbound
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
	h.grants[id] = record.GrantHash
	h.devices[id] = &deviceSlot{conn: conn, secretHash: hash, inbound: record.Inbound, online: true}
	return ""
}

func (h *Hub) RegisterClient(targetID, grant string, conn Sender) string {
	ctx, release, err := h.beginIdentity(conn, targetID)
	if err != nil {
		return identityErrorCode(err)
	}
	defer release()
	want := protocol.SHA256Hex(grant)
	var record DeviceRecord
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
	gh := h.grants[targetID]
	if h.store != nil {
		gh = record.GrantHash
		h.grants[targetID] = gh
	} else if h.devices[targetID] == nil {
		return protocol.UnknownDevice
	}
	if gh == "" || gh != want {
		return protocol.Unauthorized
	}
	if err := conn.Enqueue(protocol.Message{Type: protocol.TypeHelloOK, Role: protocol.RoleClient}); err != nil {
		return protocol.RelayError
	}
	h.clients[conn] = clientSlot{targetID: targetID, grantHex: want}
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
