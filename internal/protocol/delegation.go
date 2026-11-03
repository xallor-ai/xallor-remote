package protocol

import "time"

const (
	TypeExecutionRenew      = "execution.renew"
	TypeExecutionLease      = "execution.lease"
	MaxDelegationBytes      = 4096
	DelegationProofLifetime = 11 * time.Minute
	ExecutionLeaseDuration  = 20 * time.Second
	ExecutionRenewInterval  = 5 * time.Second
	ExecutionRenewTimeout   = 5 * time.Second
)
