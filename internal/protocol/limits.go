package protocol

import "time"

const (
	MaxFrameBytes       = 64 * 1024
	MaxExecOutputBytes  = 4 * 1024 * 1024
	MaxRelayBufferBytes = 1 * 1024 * 1024
	MaxWriteBytes       = 1 * 1024 * 1024
	MaxReadBytes        = 1 * 1024 * 1024
	MaxReadOffsetBytes  = 1 * 1024 * 1024 * 1024
	DefaultReadBytes    = 64 * 1024
	MaxPathBytes        = 4096
	MaxEnvelopeBytes    = 8192
	MaxWSMessageBytes   = 6*(MaxWriteBytes+MaxPathBytes) + MaxEnvelopeBytes
	MaxConcurrentExec   = 4
	HeartbeatEvery      = 30 * time.Second
	ConnStaleAfter      = 60 * time.Second
)

const (
	GrantPrefix = "xr_grant_"
)
