package relay

import (
	"github.com/xallor-ai/xallor-remote/internal/access"
	"github.com/xallor-ai/xallor-remote/internal/invocation"
)

func (h *Hub) deviceContext(record access.Device) *invocation.DeviceContext {
	binding := record.Binding()
	current := &invocation.DeviceContext{DeviceUID: binding.DeviceUID, OwnershipID: binding.OwnershipID, GrantID: binding.GrantID, Inbound: record.Inbound}
	if h.store != nil {
		current.Authority = h.store.Authority()
	}
	return current
}
