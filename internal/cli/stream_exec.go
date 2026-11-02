package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/xallor-ai/xallor-remote/internal/execution"
	"github.com/xallor-ai/xallor-remote/internal/ipc"
	"github.com/xallor-ai/xallor-remote/internal/protocol"
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
	return consumeExec(c.ReadFrame, os.Stdout, os.Stderr)
}

func consumeExec(read func() (ipc.Frame, error), stdout, stderr io.Writer) error {
	for {
		fr, err := read()
		if err != nil {
			return fmt.Errorf("未收到任务终态，执行结果未确认：%w", err)
		}
		if fr.ID != "e1" {
			return fmt.Errorf("任务响应不匹配，执行结果未确认。")
		}
		if fr.Event == "stdout" {
			if _, err = fmt.Fprint(stdout, fr.Data); err != nil {
				return err
			}
			continue
		}
		if fr.Event == "stderr" {
			if _, err = fmt.Fprint(stderr, fr.Data); err != nil {
				return err
			}
			continue
		}
		if fr.OK == nil {
			return fmt.Errorf("任务响应无效，执行结果未确认。")
		}
		if !*fr.OK {
			return fmt.Errorf("%s", ipc.Human(fr.Code))
		}
		var result struct {
			Status    string `json:"status"`
			ExitCode  *int   `json:"exit_code"`
			Truncated bool   `json:"truncated"`
		}
		if json.Unmarshal(fr.Result, &result) != nil {
			return fmt.Errorf("任务结果无效。")
		}
		switch result.Status {
		case protocol.ExitCancelled:
			return fmt.Errorf("%s", ipc.Human(protocol.Cancelled))
		case protocol.ExitTimeout:
			return fmt.Errorf("%s", ipc.Human(protocol.ExecTimeout))
		case protocol.ExitCompleted:
			if result.ExitCode == nil {
				return fmt.Errorf("缺少退出码，执行结果未确认。")
			}
			if *result.ExitCode != 0 {
				return fmt.Errorf("任务失败，退出码 %d。", *result.ExitCode)
			}
			if result.Truncated {
				fmt.Fprintln(stderr, "输出超过上限，已截断。")
			}
			return nil
		default:
			return fmt.Errorf("缺少有效终态，执行结果未确认。")
		}
	}
}
