package ipc

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/xallor-ai/xallor-remote/internal/protocol"
)

// ConsumeExec renders an execution stream for Go heads. A closed stream or
// transport acknowledgement is not a successful process terminal.
func ConsumeExec(requestID string, read func() (Frame, error), stdout, stderr io.Writer) error {
	for {
		fr, err := read()
		if err != nil {
			return fmt.Errorf("未收到任务终态，执行结果未确认：%w", err)
		}
		if fr.ID != requestID {
			return fmt.Errorf("任务响应不匹配，执行结果未确认。")
		}
		if fr.Event != "" && (fr.OK != nil || fr.Code != "" || len(fr.Result) != 0 || fr.Event != "stdout" && fr.Event != "stderr") {
			return fmt.Errorf("任务事件无效，执行结果未确认。")
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
			return fmt.Errorf("%s", Human(fr.Code))
		}
		if fr.Code != "" {
			return fmt.Errorf("任务响应矛盾，执行结果未确认。")
		}
		var result struct {
			Status    string `json:"status"`
			ExitCode  *int   `json:"exit_code"`
			Truncated bool   `json:"truncated"`
		}
		if json.Unmarshal(fr.Result, &result) != nil {
			return fmt.Errorf("任务结果无效。")
		}
		if result.Truncated {
			if _, err := fmt.Fprintln(stderr, "输出超过上限，已截断。"); err != nil {
				return err
			}
		}
		switch result.Status {
		case protocol.ExitCancelled:
			return fmt.Errorf("%s", Human(protocol.Cancelled))
		case protocol.ExitTimeout:
			return fmt.Errorf("%s", Human(protocol.ExecTimeout))
		case protocol.ExitCompleted:
			if result.ExitCode == nil {
				return fmt.Errorf("缺少退出码，执行结果未确认。")
			}
			if *result.ExitCode != 0 {
				return fmt.Errorf("任务失败，退出码 %d。", *result.ExitCode)
			}
			return nil
		default:
			return fmt.Errorf("缺少有效终态，执行结果未确认。")
		}
	}
}
