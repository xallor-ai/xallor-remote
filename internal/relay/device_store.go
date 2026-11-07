package relay

import (
	"context"

	"github.com/xallor-ai/xallor-remote/internal/access"
)

// DeviceStore must commit a mutation before returning success. Registration
// atomically creates an absent identity or authenticates the existing one.
// Operational errors must never be returned as access.ErrDeviceNotFound.
type DeviceStore interface {
	// Authority is immutable, loaded when the store opens; it must not perform I/O.
	Authority() string
	RegisterDevice(ctx context.Context, id, secretHash string, inbound bool) (access.Device, error)
	LoadDevice(ctx context.Context, id string) (access.Device, error)
	UpdateDevice(ctx context.Context, id string, record access.Device) (access.Device, error)
	DeleteDevice(ctx context.Context, id, uid, secretHash string) error
}
