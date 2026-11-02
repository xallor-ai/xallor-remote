// Package access owns device identity and the binding of a sharing credential.
// Display numbers route connections; they are not durable authorization identities.
package access

import (
	"crypto/rand"
	"encoding/hex"
)

// Device is a committed identity/access snapshot. UID survives reconnection and
// is replaced on recreation. GrantID changes only when the sharing hash changes.
type Device struct {
	UID        string
	SecretHash string
	Inbound    bool
	GrantID    string
	GrantHash  string
}

// Binding identifies what was authenticated, without retaining a bearer secret.
// Tenant/principal authorization will produce its own trusted context; a sharing
// binding alone does not identify a user or a billing tenant.
type Binding struct {
	DeviceUID string
	GrantID   string
}

func (d Device) Binding() Binding { return Binding{DeviceUID: d.UID, GrantID: d.GrantID} }

func (b Binding) Matches(current Binding) bool {
	return b.DeviceUID != "" && b.GrantID != "" && b == current
}

func NewDevice(secretHash string, inbound bool) (Device, error) {
	id, err := newID()
	return Device{UID: id, SecretHash: secretHash, Inbound: inbound}, err
}

// WithAccess retains the binding on idempotent synchronization. Reusing an older
// hash after an intervening change creates a fresh grant, never resurrecting it.
func (d Device) WithAccess(grantHash string, inbound bool) (Device, error) {
	d.Inbound = inbound
	if grantHash == d.GrantHash {
		return d, nil
	}
	d.GrantHash, d.GrantID = grantHash, ""
	if grantHash != "" {
		id, err := newID()
		if err != nil {
			return Device{}, err
		}
		d.GrantID = id
	}
	return d, nil
}

func newID() (string, error) {
	var bytes [16]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes[:]), nil
}
