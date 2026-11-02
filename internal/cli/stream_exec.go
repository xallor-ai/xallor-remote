package cli

import (
	"encoding/json"
	"io"
	"time"

	"github.com/xallor-ai/xallor-remote/internal/execution"
	"github.com/xallor-ai/xallor-remote/internal/ipc"
	"github.com/xallor-ai/xallor-remote/internal/protocol"
)

func streamExec(device string, request execution.Request, jsonl bool, stdout, stderr io.Writer) error {
	raw, err := ipc.Dial(2 * time.Second)
	if err != nil {
		return localExecError(stdout, jsonl, protocol.RuntimeUnavailable, err)
	}
	defer raw.Close()
	c := ipc.NewConn(raw)
	params := struct {
		DeviceID string `json:"device_id,omitempty"`
		execution.Request
	}{DeviceID: device, Request: request}
	if err := ipc.Call(c, "e1", "exec", params); err != nil {
		return localExecError(stdout, jsonl, protocol.ResultUnknown, err)
	}
	if jsonl {
		encoder := json.NewEncoder(stdout)
		return ipc.ReadExec("e1", c.ReadFrame, func(event ipc.ExecEvent) error { return encoder.Encode(event) })
	}
	return ipc.ConsumeExec("e1", c.ReadFrame, stdout, stderr)
}

func localExecError(stdout io.Writer, jsonl bool, code string, cause error) error {
	if jsonl {
		if err := json.NewEncoder(stdout).Encode(ipc.ExecEvent{Type: "error", Code: code, Message: cause.Error()}); err != nil {
			return err
		}
	}
	return cause
}
