package protocol

// OperationPolicy is the stable, transport-independent contract for a remote
// operation. It describes the checks and result boundary; it is not a grant or
// a commercial entitlement and never replaces Relay's per-call authorization.
type OperationPolicy struct {
	Operation      string `json:"operation"`
	Authorization  string `json:"authorization"`
	TargetBoundary string `json:"target_boundary"`
	Approval       string `json:"approval"`
	Terminal       string `json:"terminal"`
	Retry          string `json:"retry"`
}

type OperationPolicyMatrix struct {
	Version    int               `json:"version"`
	Sandbox    string            `json:"sandbox"`
	Operations []OperationPolicy `json:"operations"`
}

// PolicyMatrix returns the one operation contract shared by protocol, CLI,
// MCP, GUI and the target Runtime. Callers receive fresh slices so a UI cannot
// mutate the process-wide definition.
func PolicyMatrix() OperationPolicyMatrix {
	return OperationPolicyMatrix{
		Version: 1,
		Sandbox: "none",
		Operations: []OperationPolicy{
			{Operation: OpInfo, Authorization: "info permission + current device access", TargetBoundary: "target metadata; no command", Approval: "none", Terminal: "exit or nack", Retry: "safe only after confirmed terminal"},
			{Operation: OpRead, Authorization: "read permission + current device access", TargetBoundary: "workspace read window", Approval: "none", Terminal: "exit or nack", Retry: "safe only after confirmed terminal"},
			{Operation: OpWrite, Authorization: "write permission + current device access", TargetBoundary: "workspace atomic replacement", Approval: "none", Terminal: "exit or nack", Retry: "not automatic after unknown"},
			{Operation: OpExec, Authorization: "exec permission + current device access", TargetBoundary: "target shell/program under native-user supervisor", Approval: "Runtime denylist or subscribed human approval", Terminal: "exit, nack, or error", Retry: "never after unknown"},
			{Operation: OpProcesses, Authorization: "processes permission + current device access", TargetBoundary: "target current-user process view", Approval: "none", Terminal: "exit or nack", Retry: "safe only after confirmed terminal"},
			{Operation: OpCancel, Authorization: "original client connection owns exec", TargetBoundary: "that connection's in-flight task", Approval: "none", Terminal: "cancelled exit or unknown_exec", Retry: "never for an unknown task"},
			{Operation: OpHandoffSubmit, Authorization: "handoff.submit permission + fresh delegation", TargetBoundary: "durable Agent session under native-user supervisor", Approval: "handoff preflight policy", Terminal: "receipt state", Retry: "never after unknown"},
			{Operation: OpHandoffGet, Authorization: "handoff.get permission + matching scope", TargetBoundary: "stored receipt only", Approval: "none", Terminal: "receipt or error", Retry: "safe only after confirmed terminal"},
		},
	}
}
