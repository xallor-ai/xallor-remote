package relay

import "github.com/xallor-ai/xallor-remote/internal/protocol"

func (h *Hub) handleAuth(from Sender, msg protocol.Message) {
	h.mu.Lock()
	var id string
	for did, d := range h.devices {
		if d.conn == from {
			id = did
			break
		}
	}
	h.mu.Unlock()
	if id == "" {
		return
	}
	ctx, release, err := h.beginIdentity(from, id)
	if err != nil {
		h.rejectIdentity(from, identityErrorCode(err))
		return
	}
	defer release()
	h.mu.Lock()
	d := h.devices[id]
	if d == nil || d.conn != from {
		h.mu.Unlock()
		return
	}
	record := DeviceRecord{SecretHash: d.secretHash, GrantHash: h.grants[id], Inbound: d.inbound}
	h.mu.Unlock()
	switch msg.Type {
	case protocol.TypeGrantRotate:
		record.GrantHash = msg.NewGrantHash
	case protocol.TypeInboundSet:
		if msg.Inbound == nil {
			return
		}
		record.Inbound = *msg.Inbound
	case protocol.TypeRevoke:
	default:
		return
	}
	if h.store != nil {
		if msg.Type == protocol.TypeRevoke {
			err = h.store.DeleteDevice(ctx, id, record.SecretHash)
		} else {
			err = h.store.UpdateDevice(ctx, id, record)
		}
	}
	if err != nil {
		h.rejectIdentity(from, identityErrorCode(err))
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	// Drop may run during I/O. Apply a committed change to the same identity
	// even if it went offline, but never resurrect or overwrite a replacement.
	if h.devices[id] != d {
		return
	}
	if msg.Type == protocol.TypeRevoke {
		h.failDeviceLocked(id, protocol.DeviceOffline)
		from.Close()
		delete(h.devices, id)
		delete(h.grants, id)
		return
	}
	d.inbound = record.Inbound
	h.grants[id] = record.GrantHash
}

func (h *Hub) rejectIdentity(from Sender, code string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	// Failed/uncertain writes stop routing immediately, before network drain.
	for id, d := range h.devices {
		if d.conn == from {
			h.failDeviceLocked(id, protocol.DeviceOffline)
			d.online = false
			d.conn = nil
			break
		}
	}
	_ = from.Enqueue(protocol.Message{Type: protocol.TypeError, Code: code})
	from.Close()
}
