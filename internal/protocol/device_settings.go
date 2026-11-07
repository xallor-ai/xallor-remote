package protocol

import "encoding/hex"

// Device settings are a complete snapshot, never two independently applied
// changes. Empty grants are valid only with inbound disabled.
func (m Message) ValidDeviceSettings() bool {
	if m.RequestID == "" || len(m.RequestID) > 128 || m.Inbound == nil {
		return false
	}
	if m.GrantHash == "" {
		return !*m.Inbound
	}
	if len(m.GrantHash) != 64 {
		return false
	}
	b, err := hex.DecodeString(m.GrantHash)
	return err == nil && hex.EncodeToString(b) == m.GrantHash
}
