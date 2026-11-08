package protocol

const (
	Unauthorized        = "unauthorized"
	UnknownDevice       = "unknown_device"
	UnknownExec         = "unknown_exec"
	DeviceOffline       = "device_offline"
	InboundDisabled     = "inbound_disabled"
	PolicyDeny          = "policy_deny"
	ApprovalTimeout     = "approval_timeout"
	ExecTimeout         = "exec_timeout"
	Cancelled           = "cancelled"
	RelayError          = "relay_error"
	AgentError          = "agent_error"
	ResultUnknown       = "result_unknown"
	RuntimeUnavailable  = "runtime_unavailable"
	UnsupportedShell    = "unsupported_shell"
	ShellUnavailable    = "shell_unavailable"
	InvalidExecution    = "invalid_execution"
	ProgramUnavailable  = "program_unavailable"
	UnsupportedProgram  = "unsupported_program"
	QuotaExceeded       = "quota_exceeded"
	ResourceUncertain   = "resource_uncertain"
	ResourceUnavailable = "resource_unavailable"
	WorkspaceMissing    = "workspace_missing"
	TooLarge            = "too_large"
)

const (
	TypeHelloDevice          = "hello_device"
	TypeHelloClient          = "hello_client"
	TypeHelloOK              = "hello_ok"
	TypeHeartbeat            = "heartbeat"
	TypeInvoke               = "invoke"
	TypeInvokeNack           = "invoke_nack"
	TypeDeviceSettings       = "device.settings"
	TypeRevoke               = "revoke"
	TypeDeviceSettingsResult = "device.settings.result"
	TypeStdout               = "stdout"
	TypeStderr               = "stderr"
	TypeExit                 = "exit"
	TypeError                = "error"
	TypeAccessRequest        = "access.request"
	TypeAccessResult         = "access.result"
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
	ExitCompleted         = "completed"
	ExitCancelled         = "cancelled"
	ExitTimeout           = "timeout"
	ExitAuthorizationLost = "authorization_lost"
	RoleDevice            = "device"
	RoleClient            = "client"
)
