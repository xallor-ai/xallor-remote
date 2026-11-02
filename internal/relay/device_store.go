package relay

import (
	"context"
	"errors"
)

var (
	ErrDeviceNotFound = errors.New("device not found")
	ErrDeviceIdentity = errors.New("device identity does not match")
)

// DeviceRecord is the authoritative persisted device identity and access state.
type DeviceRecord struct {
	SecretHash string
	GrantHash  string
	Inbound    bool
}

// DeviceStore must commit a mutation before returning success. Registration
// atomically creates an absent identity or authenticates the existing one.
// Operational errors must never be returned as ErrDeviceNotFound.
type DeviceStore interface {
	RegisterDevice(ctx context.Context, id, secretHash string, inbound bool) (DeviceRecord, error)
	LoadDevice(ctx context.Context, id string) (DeviceRecord, error)
	UpdateDevice(ctx context.Context, id string, record DeviceRecord) error
	DeleteDevice(ctx context.Context, id, secretHash string) error
}
