package cli

import (
	"os"
	"time"

	"github.com/xallor-ai/xallor-remote/internal/execution"
	"github.com/xallor-ai/xallor-remote/internal/ipc"
)

func streamExec(device string, request execution.Request) error {
	raw, err := ipc.Dial(2 * time.Second)
	if err != nil {
		return err
	}
	defer raw.Close()
	c := ipc.NewConn(raw)
	params := struct {
		DeviceID string `json:"device_id,omitempty"`
		execution.Request
	}{DeviceID: device, Request: request}
	if err := ipc.Call(c, "e1", "exec", params); err != nil {
		return err
	}
	return ipc.ConsumeExec("e1", c.ReadFrame, os.Stdout, os.Stderr)
}
