package noderun

import (
	"github.com/xallor-ai/xallor-remote/internal/delegation"
	"github.com/xallor-ai/xallor-remote/internal/protocol"
)

// Called on the device reader before dispatching later frames or acknowledging
// management to IPC. Background submit goroutines recheck under Manager's lock.
func (d *Daemon) syncDeviceContext(msg protocol.Message) error {
	if msg.DeviceContext == nil || !msg.DeviceContext.Valid() {
		d.revokeLocalExecs(delegation.ErrDenied)
		return errStr("device execution context unconfirmed")
	}
	var syncErr error
	if d.handoffs != nil {
		syncErr = d.handoffs.SyncDevice(*msg.DeviceContext)
	}
	if !msg.DeviceContext.Inbound || syncErr != nil {
		d.revokeLocalExecs(delegation.ErrDenied)
	}
	return syncErr
}

func (d *Daemon) revokeLocalExecs(cause error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	for _, cancel := range d.local {
		cancel(cause)
	}
}
