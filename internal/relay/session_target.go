package relay

import (
	"context"
	"time"

	"github.com/xallor-ai/xallor-remote/internal/accesscode"
	"github.com/xallor-ai/xallor-remote/internal/protocol"
)

// Caller holds h.mu. Stop routing before persistent epoch changes, and retain
// the binding so cleanup remains scoped even after the slot is replaced/deleted.
func (h *Hub) retireDeviceLocked(id string, d *deviceSlot) accesscode.TargetBinding {
	h.failDeviceLocked(id, protocol.DeviceOffline)
	d.online, d.ready, d.conn = false, false, nil
	for key, pending := range h.pendingSessions {
		if pending.targetID == id {
			delete(h.pendingSessions, key)
		}
	}
	for conn, client := range h.clients {
		if client.targetID == id && (client.session != nil || client.sessionRequired) {
			delete(h.clients, conn)
			conn.Close()
		}
	}
	return d.sessionTarget
}

// No routing mutex is held during SQL I/O. Failure does not undo the in-memory
// fence. A future registration must commit a fresh epoch before routing resumes.
func (h *Hub) retireSessionTarget(target accesscode.TargetBinding) error {
	if h.accessCodes == nil || !target.Valid() {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), protocol.RelayIdentityTimeout)
	defer cancel()
	return h.accessCodes.WriteAccessCode(ctx, func(tx accesscode.Transaction) error {
		return tx.RetireSessionTarget(ctx, target, time.Now().UTC())
	})
}

func (h *Hub) activateSessionTarget(ctx context.Context, uid string) (accesscode.TargetBinding, error) {
	if h.accessCodes == nil {
		return accesscode.TargetBinding{}, nil
	}
	target, err := accesscode.NewTargetBinding(uid)
	if err != nil {
		return accesscode.TargetBinding{}, err
	}
	err = h.accessCodes.WriteAccessCode(ctx, func(tx accesscode.Transaction) error {
		return tx.ActivateSessionTarget(ctx, target, time.Now().UTC())
	})
	return target, err
}
