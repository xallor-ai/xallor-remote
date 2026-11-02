package noderun

import (
	"github.com/xallor-ai/xallor-remote/internal/activity"
	"github.com/xallor-ai/xallor-remote/internal/protocol"
)

func (d *Daemon) recordActivity(e activity.Entry) {
	if d.activity == nil {
		return
	}
	if err := d.activity.Append(e); err != nil && d.log != nil {
		d.log.Warn("local audit unavailable")
	}
}

func (d *Daemon) recordTerminal(direction, device string, msg protocol.Message) {
	if !isTerminal(msg) {
		return
	}
	e := activity.Entry{ExecID: msg.ExecID, Direction: direction, DeviceID: device, ExitCode: msg.ExitCode, Code: msg.Code}
	switch msg.Type {
	case protocol.TypeExit:
		e.Stage = "finished"
		e.Code = msg.Status
	case protocol.TypeInvokeNack:
		e.Stage = "rejected"
	default:
		e.Stage = "unknown"
	}
	d.recordActivity(e)
}
