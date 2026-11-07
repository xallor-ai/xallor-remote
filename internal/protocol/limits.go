package protocol

import "time"

const (
	MaxFrameBytes          = 64 * 1024
	MaxExecOutputBytes     = 4 * 1024 * 1024
	MaxRelayBufferBytes    = 1 * 1024 * 1024
	MaxRelayQueueFrames    = 256
	MaxRelayInboxFrames    = 64
	MaxRelayInboxBytes     = MaxWSMessageBytes + MaxRelayBufferBytes
	MaxRelayQueueBaseBytes = 4 * 1024 * 1024
	MaxRelayQueueBytes     = MaxRelayQueueBaseBytes + MaxWSMessageBytes
	// Transport budgets are independent of local jobs and commercial entitlements.
	MaxDeviceRequests         = 16
	RelayControlReserveFrames = 2 * MaxDeviceRequests
	RelayControlReserveBytes  = MaxEnvelopeBytes * MaxDeviceRequests
	RelayWriteTimeout         = 5 * time.Second
	RelayIdentityTimeout      = 5 * time.Second
	MaxWriteBytes             = 1 * 1024 * 1024
	MaxReadBytes              = 1 * 1024 * 1024
	MaxReadOffsetBytes        = 1 * 1024 * 1024 * 1024
	MaxReadLines              = 1024
	DefaultReadBytes          = 64 * 1024
	MaxPathBytes              = 4096
	MaxEnvelopeBytes          = 8192
	MaxWSMessageBytes         = 6*(MaxWriteBytes+MaxPathBytes) + MaxEnvelopeBytes
	MaxIPCMessageBytes        = 6*(MaxWriteBytes+MaxPathBytes) + MaxEnvelopeBytes
	MaxDeviceShares           = 128
	HeartbeatEvery            = 30 * time.Second
	ConnStaleAfter            = 60 * time.Second
)

const (
	GrantPrefix = "xr_grant_"
)
