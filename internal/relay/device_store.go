package relay

import (
	"context"
	"errors"

	"github.com/xallor-ai/xallor-remote/internal/access"
)

var (
	ErrDeviceNotFound = errors.New("device not found")
	ErrDeviceIdentity = errors.New("device identity does not match")
)

// DeviceStore must commit a mutation before returning success. Registration
// atomically creates an absent identity or authenticates the existing one.
// Operational errors must never be returned as ErrDeviceNotFound.
type DeviceStore interface {
	RegisterDevice(ctx context.Context, id, secretHash string, inbound bool) (access.Device, error)
	LoadDevice(ctx context.Context, id string) (access.Device, error)
	UpdateDevice(ctx context.Context, id string, record access.Device) (access.Device, error)
	DeleteDevice(ctx context.Context, id, uid, secretHash string) error
}
