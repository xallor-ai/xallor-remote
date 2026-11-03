package relay

import (
	"context"

	"github.com/xallor-ai/xallor-remote/internal/access"
	"github.com/xallor-ai/xallor-remote/internal/principal"
)

// DeviceAccess is the router's application boundary. Implementations own access
// decisions and transactional revalidation; Hub owns connection proof and route
// publication. Storage operations are deliberately absent from this interface.
type DeviceAccess interface {
	Resolve(context.Context, access.Device, principal.Identity) (access.Authorization, error)
	Verify(context.Context, principal.Identity, access.Authorization, string) error
	Manage(context.Context, string, access.Device, principal.Identity, access.Request) (access.Mutation, error)
}

var _ DeviceAccess = (*access.Service)(nil)
