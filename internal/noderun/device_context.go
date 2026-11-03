package noderun

import "github.com/xallor-ai/xallor-remote/internal/protocol"

// Called on the device reader before dispatching later frames or acknowledging
// management to IPC. Background submit goroutines recheck under Manager's lock.
func (d *Daemon) syncDeviceContext(msg protocol.Message) error {
	if msg.DeviceContext == nil || !msg.DeviceContext.Valid() {
		return errStr("device execution context unconfirmed")
	}
	if d.handoffs != nil {
		return d.handoffs.SyncDevice(*msg.DeviceContext)
	}
	return nil
}
