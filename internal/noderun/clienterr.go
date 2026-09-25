package noderun

import "github.com/xallor-ai/xallor-remote/internal/protocol"

func clientDialCode(err error) string {
	if err == nil {
		return ""
	}
	switch err.Error() {
	case protocol.Unauthorized, protocol.UnknownDevice, protocol.DeviceOffline, protocol.InboundDisabled, protocol.QuotaExceeded:
		return err.Error()
	default:
		return protocol.RelayError
	}
}
