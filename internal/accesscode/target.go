package accesscode

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
)

var ErrTargetEpoch = errors.New("recipient connection epoch is no longer active")

// TargetBinding is issued by the Relay, never supplied by a remote caller.
// It binds recipient consent to an authenticated routing/settings incarnation.
type TargetBinding struct {
	DeviceUID string `json:"device_uid"`
	Epoch     string `json:"epoch"`
}

func (b TargetBinding) Valid() bool { return b.DeviceUID != "" && b.Epoch != "" }

// Generate before entering a retryable transaction; a retry retains the same
// proposed epoch and only retries after a confirmed rollback.
func NewTargetBinding(uid string) (TargetBinding, error) {
	if uid == "" {
		return TargetBinding{}, ErrTargetEpoch
	}
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return TargetBinding{}, err
	}
	return TargetBinding{DeviceUID: uid, Epoch: hex.EncodeToString(nonce[:])}, nil
}
