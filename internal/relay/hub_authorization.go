package relay

import "github.com/xallor-ai/xallor-remote/internal/protocol"

func (h *Hub) handleAuth(from Sender, msg protocol.Message) {
	if msg.Type == protocol.TypeDeviceSettings && !msg.ValidDeviceSettings() {
		key := msg.RequestID
		if len(key) > 128 {
			key = ""
		}
		_ = from.Enqueue(protocol.Message{Type: protocol.TypeDeviceSettingsResult, RequestID: key, Code: protocol.AgentError})
		return
	}
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
	record := d.record
	h.mu.Unlock()
	switch msg.Type {
	case protocol.TypeDeviceSettings:
		record.GrantHash = msg.GrantHash
		record.Inbound = *msg.Inbound
	case protocol.TypeRevoke:
	default:
		return
	}
	if h.store != nil {
		if msg.Type == protocol.TypeRevoke {
			err = h.store.DeleteDevice(ctx, id, record.UID, record.SecretHash)
		} else {
			record, err = h.store.UpdateDevice(ctx, id, record)
		}
	} else if msg.Type != protocol.TypeRevoke {
		record, err = d.record.WithAccess(record.GrantHash, record.Inbound)
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
		delete(h.access, id)
		return
	}
	wasInbound := d.record.Inbound
	d.record = record
	h.access[id] = record.Binding()
	if wasInbound && !record.Inbound {
		h.revokeForegroundRoutesLocked(id)
	}
	// Queue the confirmation before publishing readiness, so initial invokes
	// cannot precede the settings response on the shared device connection.
	if err := from.Enqueue(protocol.Message{Type: protocol.TypeDeviceSettingsResult, RequestID: msg.RequestID, DeviceContext: h.deviceContext(record)}); err != nil {
		h.stopDeviceRoutingLocked(from)
		from.Close()
		return
	}
	d.ready = true
}

func (h *Hub) rejectIdentity(from Sender, code string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.stopDeviceRoutingLocked(from)
	_ = from.Enqueue(protocol.Message{Type: protocol.TypeError, Code: code})
	from.Close()
}

// Existing foreground execs keep their route until a real terminal, but cannot
// be renewed after inbound is revoked—even if inbound is later re-enabled.
func (h *Hub) revokeForegroundRoutesLocked(id string) {
	for _, task := range h.inflight {
		if task.deviceID == id && task.operation == protocol.OpExec {
			task.cancelled = true
		}
	}
}

func (h *Hub) stopDeviceRoutingLocked(from Sender) {
	// Failed/uncertain writes stop routing immediately, before network drain.
	for id, d := range h.devices {
		if d.conn == from {
			h.failDeviceLocked(id, protocol.DeviceOffline)
			d.online = false
			d.conn = nil
			break
		}
	}
}
