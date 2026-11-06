package relay

import (
	"errors"

	"github.com/xallor-ai/xallor-remote/internal/protocol"
)

func (h *Hub) RegisterDevice(id, secret string, inbound bool, conn Sender) string {
	hash := protocol.SHA256Hex(secret)
	h.mu.Lock()
	defer h.mu.Unlock()
	record := DeviceRecord{SecretHash: hash, GrantHash: h.grants[id], Inbound: inbound}
	if h.store != nil {
		var err error
		record, err = h.store.RegisterDevice(id, hash, inbound)
		if err != nil {
			return identityErrorCode(err)
		}
	} else if old := h.devices[id]; old != nil {
		if old.secretHash != hash {
			return protocol.Unauthorized
		}
		record.Inbound = old.inbound
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
	want := protocol.SHA256Hex(grant)
	h.mu.Lock()
	defer h.mu.Unlock()
	gh := h.grants[targetID]
	if h.store != nil {
		record, err := h.store.LoadDevice(targetID)
		if err != nil {
			return identityErrorCode(err)
		}
		gh = record.GrantHash
		h.grants[targetID] = gh
	} else if h.devices[targetID] == nil {
		return protocol.UnknownDevice
	}
	if gh == "" || gh != want {
		return protocol.Unauthorized
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
	d := h.devices[id]
	record := DeviceRecord{SecretHash: d.secretHash, GrantHash: h.grants[id], Inbound: d.inbound}
	var err error
	switch msg.Type {
	case protocol.TypeGrantRotate:
		record.GrantHash = msg.NewGrantHash
	case protocol.TypeInboundSet:
		if msg.Inbound == nil {
			return
		}
		record.Inbound = *msg.Inbound
	case protocol.TypeRevoke:
		if h.store != nil {
			err = h.store.DeleteDevice(id, record.SecretHash)
		}
	}
	if h.store != nil && msg.Type != protocol.TypeRevoke {
		err = h.store.UpdateDevice(id, record)
	}
	if err != nil {
		// Persisted and live authorization must not diverge on a failed write.
		_ = from.Send(protocol.Message{Type: protocol.TypeError, Code: identityErrorCode(err)})
		from.Close()
		return
	}
	if msg.Type == protocol.TypeRevoke {
		h.failDeviceLocked(id, protocol.DeviceOffline)
		d.conn.Close()
		delete(h.devices, id)
		delete(h.grants, id)
		return
	}
	d.inbound = record.Inbound
	h.grants[id] = record.GrantHash
}
