package protocol

const (
	Unauthorized       = "unauthorized"
	UnknownDevice      = "unknown_device"
	UnknownExec        = "unknown_exec"
	DeviceOffline      = "device_offline"
	InboundDisabled    = "inbound_disabled"
	PolicyDeny         = "policy_deny"
	ApprovalTimeout    = "approval_timeout"
	ExecTimeout        = "exec_timeout"
	Cancelled          = "cancelled"
	RelayError         = "relay_error"
	AgentError         = "agent_error"
	UnsupportedShell   = "unsupported_shell"
	ShellUnavailable   = "shell_unavailable"
	InvalidExecution   = "invalid_execution"
	ProgramUnavailable = "program_unavailable"
	UnsupportedProgram = "unsupported_program"
	QuotaExceeded      = "quota_exceeded"
	WorkspaceMissing   = "workspace_missing"
	TooLarge           = "too_large"
)

const (
	TypeHelloDevice   = "hello_device"
	TypeHelloClient   = "hello_client"
	TypeHelloOK       = "hello_ok"
	TypeHeartbeat     = "heartbeat"
	TypeInvoke        = "invoke"
	TypeInvokeNack    = "invoke_nack"
	TypeGrantRotate   = "grant_rotate"
	TypeRevoke        = "revoke"
	TypeInboundSet    = "inbound_set"
	TypeStdout        = "stdout"
	TypeStderr        = "stderr"
	TypeExit          = "exit"
	TypeError         = "error"
	TypeAccessRequest = "access.request"
	TypeAccessResult  = "access.result"
)

const (
	OpExec          = "exec"
	OpCancel        = "cancel"
	OpRead          = "read"
	OpWrite         = "write"
	OpProcesses     = "processes"
	OpInfo          = "info"
	OpHandoffSubmit = "handoff.submit"
	OpHandoffGet    = "handoff.get"
)

const (
	ExitCompleted = "completed"
	ExitCancelled = "cancelled"
	ExitTimeout   = "timeout"
	RoleDevice    = "device"
	RoleClient    = "client"
)
