package tui

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/xallor-ai/xallor-remote/internal/execution"
	"github.com/xallor-ai/xallor-remote/internal/ipc"
)

func rpc(method string, params map[string]any) (map[string]any, error) {
	raw, err := ipc.Dial(2 * time.Second)
	if err != nil {
		return nil, fmt.Errorf("Runtime 未运行，请先 xallor-remote start 或 ensure")
	}
	defer raw.Close()
	c := ipc.NewConn(raw)
	if err := ipc.Call(c, "1", method, params); err != nil {
		return nil, err
	}
	fr, err := c.ReadFrame()
	if err != nil {
		return nil, err
	}
	if fr.OK == nil || !*fr.OK {
		return nil, fmt.Errorf("%s", ipc.Human(fr.Code))
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

func streamExec(command string, onOut func(string)) error {
	raw, err := ipc.Dial(2 * time.Second)
	if err != nil {
		return fmt.Errorf("Runtime 未运行，请先 xallor-remote start 或 ensure")
	}
	defer raw.Close()
	c := ipc.NewConn(raw)
	if err := ipc.Call(c, "e1", "exec", execution.Request{Script: &execution.Script{Text: command}}); err != nil {
		return err
	}
	writer := outputWriter(onOut)
	return ipc.ConsumeExec("e1", c.ReadFrame, writer, writer)
}

type outputWriter func(string)

func (w outputWriter) Write(b []byte) (int, error) {
	if len(b) > 0 {
		w(string(b))
	}
	return len(b), nil
}

func peerIDs(res map[string]any) []string {
	raw, _ := res["peers"].([]any)
	ids := make([]string, 0, len(raw))
	for _, p := range raw {
		if s, ok := p.(string); ok {
			ids = append(ids, s)
		}
	}
	return ids
}
