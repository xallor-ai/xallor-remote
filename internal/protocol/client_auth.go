package protocol

const (
	ClientAuthShared  = "shared"
	ClientAuthAccount = "account"
)

// ClientAuthentication confirms the authentication path completed by the Relay.
// It is neither an access grant nor a substitute for per-invocation verification.
type ClientAuthentication struct {
	Mode string `json:"mode"`
}

// ConfirmsClientAuth requires one explicit contract for every client connection.
// A generic greeting, wrong role, contradictory error or different mode cannot
// become a pooled connection or a successful authorization diagnosis.
func (m Message) ConfirmsClientAuth(account bool) bool {
	mode := ClientAuthShared
	if account {
		mode = ClientAuthAccount
	}
	return m.Type == TypeHelloOK && m.Role == RoleClient && m.Code == "" &&
		m.ClientAuth != nil && m.ClientAuth.Mode == mode
}
