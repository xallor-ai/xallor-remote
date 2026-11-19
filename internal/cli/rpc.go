package cli

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/xallor-ai/xallor-remote/internal/ipc"
	"github.com/xallor-ai/xallor-remote/internal/runtimehead"
)

// One admitted connection carries one business request. A lost reply never
// authorizes reconnecting and replaying a consent or management operation.
func rpc(method string, params map[string]any) (map[string]any, error) {
	raw, err := ipc.Dial(2 * time.Second)
	if err != nil {
		return nil, fmt.Errorf("Runtime 未运行，请先 xallor-remote start 或 ensure")
	}
	defer raw.Close()
	c := ipc.NewConn(raw)
	// Status remains available for diagnosing an unready owner.
	if method != "status" {
		c, err = runtimehead.Handshake(raw)
		if err != nil {
			return nil, err
		}
	}
	wait := 10 * time.Second
	if method == "session.request" || method == "session.resume" {
		// Runtime waits up to 90s for a session result after preparing its Relay
		// connection. Leave bounded transport/setup margin for that same result.
		wait = 120 * time.Second
	}
	if err := raw.SetDeadline(time.Now().Add(wait)); err != nil {
		return nil, err
	}
	if err := ipc.Call(c, "1", method, params); err != nil {
		return nil, err
	}
	fr, err := c.ReadFrame()
	if err != nil {
		return nil, err
	}
	if fr.OK == nil || !*fr.OK {
		return nil, fmt.Errorf("%s", ipc.FailureMessage(fr))
	}
	var out map[string]any
	if len(fr.Result) > 0 {
		_ = json.Unmarshal(fr.Result, &out)
	}
	if out == nil {
		out = map[string]any{}
	}
	return out, nil
}
