package protocol

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
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
