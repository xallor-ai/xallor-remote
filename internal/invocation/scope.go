// Package invocation defines the stable, authority-derived execution namespace.
// It contains no credentials and does not itself confer device access.
package invocation

type Scope struct {
	Authority   string `json:"authority"`
	DeviceUID   string `json:"device_uid"`
	OwnershipID string `json:"ownership_id,omitempty"`
	GrantID     string `json:"grant_id,omitempty"`
	PrincipalID string `json:"principal_id,omitempty"`
	TenantID    string `json:"tenant_id,omitempty"`
}

// Credential, membership generation and share IDs are intentionally absent:
// renewing access must not replay a previously accepted request. A new device
// ownership is a new namespace, even when transferred back to its former owner.
func (s Scope) Valid() bool {
	if !identity(s.Authority) || !identity(s.DeviceUID) {
		return false
	}
	if (s.OwnershipID == "") == (s.GrantID == "") {
		return false
	}
	if s.OwnershipID != "" && (!identity(s.OwnershipID) || s.PrincipalID == "") {
		return false
	}
	if s.GrantID != "" && !identity(s.GrantID) {
		return false
	}
	if s.PrincipalID == "" {
		return s.TenantID == ""
	}
	return identity(s.PrincipalID) && identity(s.TenantID)
}

func identity(value string) bool {
	if len(value) != 32 {
		return false
	}
	for _, c := range value {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

func (s Scope) RetainedBytes() int {
	return len(s.Authority) + len(s.DeviceUID) + len(s.OwnershipID) + len(s.GrantID) + len(s.PrincipalID) + len(s.TenantID)
}
