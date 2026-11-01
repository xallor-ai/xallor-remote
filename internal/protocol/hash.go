package protocol

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

func SHA256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

func RandomHex(nBytes int) (string, error) {
	b := make([]byte, nBytes)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// NewDeviceID returns a 9-digit routing id.
func NewDeviceID() (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	var n uint64
	for _, x := range b {
		n = (n << 8) | uint64(x)
	}
	n = n % 1_000_000_000
	if n < 100_000_000 {
		n += 100_000_000
	}
	return fmt.Sprintf("%09d", n), nil
}

// IsNumericDeviceID reports a stable 9-digit device id.
func IsNumericDeviceID(id string) bool {
	if len(id) != 9 {
		return false
	}
	for _, c := range id {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

func NewGrant() (string, error) {
	h, err := RandomHex(24)
	if err != nil {
		return "", err
	}
	return GrantPrefix + h, nil
}

func NewSecret() (string, error) {
	return RandomHex(32)
}

func NewExecID() (string, error) {
	h, err := RandomHex(16)
	if err != nil {
		return "", err
	}
	return "ex_" + h, nil
}
