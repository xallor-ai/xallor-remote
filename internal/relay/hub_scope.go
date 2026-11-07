package relay

import "github.com/xallor-ai/xallor-remote/internal/invocation"

// Called after admission under the routing lock. Every field is derived from
// authenticated bindings; no caller-provided identity crosses this boundary.
func (h *Hub) invocationScope(client clientSlot) invocation.Scope {
	scope := invocation.Scope{
		DeviceUID: client.access.DeviceUID, OwnershipID: client.access.OwnershipID,
		GrantID: client.access.GrantID, PrincipalID: client.identity.PrincipalID,
		TenantID: client.identity.TenantID,
	}
	if h.store != nil {
		scope.Authority = h.store.Authority()
	}
	return scope
}
