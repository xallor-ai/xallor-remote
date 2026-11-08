package protocol

type PlaneContract struct {
	Version              int    `json:"version"`
	ControlEntry         string `json:"control_entry"`
	DataTransport        string `json:"data_transport"`
	ManagementOperations string `json:"management_operations"`
	DataOperations       string `json:"data_operations"`
	CredentialBoundary   string `json:"credential_boundary"`
	PersistenceBoundary  string `json:"persistence_boundary"`
	HTTPBoundary         string `json:"http_boundary"`
	MCPGUIBoundary       string `json:"mcp_gui_boundary"`
	UnknownPolicy        string `json:"unknown_policy"`
}

// ControlDataPlane returns the provider-neutral separation contract shared by
// Runtime, Relay, CLI, MCP and GUI. It is descriptive metadata, not a new
// transport or an authorization bypass.
func ControlDataPlane() PlaneContract {
	return PlaneContract{
		Version:              1,
		ControlEntry:         "CLI/TUI/GUI/MCP -> local Runtime IPC; Relay management uses device-scoped authenticated control messages",
		DataTransport:        "Runtime client WSS -> Relay -> Runtime device WSS",
		ManagementOperations: "identity, account, tenant, membership, device settings, claim, share, transfer, revoke, reset",
		DataOperations:       "invoke, exec/read/write/info/processes, stdout/stderr/exit/error, cancel",
		CredentialBoundary:   "account/grant/secret never persist in data-plane payloads; Relay derives scope and revalidates authorization per call",
		PersistenceBoundary:  "management stores durable identity/access state; data plane keeps inflight routes in memory and does not persist command/output payloads",
		HTTPBoundary:         "health and transport upgrade only; no public management API or invoke HTTP endpoint",
		MCPGUIBoundary:       "MCP and GUI are local IPC clients; neither owns a Relay WSS or bypasses Runtime",
		UnknownPolicy:        "missing management confirmation or data terminal remains unknown; no automatic mutation or side-effect replay",
	}
}
