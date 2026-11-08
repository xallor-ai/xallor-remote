package protocol

// OIDCMappingContract describes the management-plane boundary for mapping a
// verified provider subject to local account state. It is descriptive only;
// no provider discovery, browser login or JWKS network fetch runs in Runtime.
type OIDCMappingContract struct {
	Version        int      `json:"version"`
	Key            string   `json:"key"`
	Fields         []string `json:"fields"`
	Invariants     []string `json:"invariants"`
	Entry          string   `json:"entry"`
	NotImplemented []string `json:"not_implemented"`
}

func OIDCMapping() OIDCMappingContract {
	return OIDCMappingContract{
		Version:        1,
		Key:            "issuer + subject",
		Fields:         []string{"issuer", "audience", "jwks_url", "issuer_version", "tenant_id", "principal_id", "membership_id", "membership_version", "version", "active", "expires_at"},
		Invariants:     []string{"issuer and subject are unique", "membership tenant/principal/version must match local active state", "issuer and mapping versions only increase", "inactive or expired issuer or mapping fails closed", "revocation and rotation require fresh token verification and mapping lookup"},
		Entry:          "local Runtime IPC / management control plane",
		NotImplemented: []string{"issuer discovery", "JWKS HTTPS fetch", "browser redirect, PKCE, callback and logout", "MCP or GUI direct OIDC", "public management HTTP API"},
	}
}
