package ipc

import (
	"encoding/json"
	"fmt"

	"github.com/xallor-ai/xallor-remote/internal/protocol"
)

// ExecEvent is a validated head-facing observation, not a scheduling state or
// an acknowledgement that the target process has started.
type ExecEvent struct {
	Type       string `json:"type"`
	ExecID     string `json:"exec_id,omitempty"`
	Data       string `json:"data,omitempty"`
	Status     string `json:"status,omitempty"`
	ExitCode   *int   `json:"exit_code,omitempty"`
	DurationMS *int64 `json:"duration_ms,omitempty"`
	Truncated  *bool  `json:"truncated,omitempty"`
	Code       string `json:"code,omitempty"`
	Message    string `json:"message,omitempty"`
}

// ReadExec emits output as it arrives and exactly one terminal if the sink stays
// writable. Sink failures stop immediately; no second write or execution retry.
func ReadExec(requestID string, read func() (Frame, error), emit func(ExecEvent) error) error {
	execID := ""
	fail := func(code, message string) error {
		if err := emit(ExecEvent{Type: "error", ExecID: execID, Code: code, Message: message}); err != nil {
			return err
		}
		return fmt.Errorf("%s", message)
	}
	unknown := func(message string) error { return fail(protocol.ResultUnknown, message) }
	for {
		fr, err := read()
		if err != nil {
			// Retain the original error for callers using errors.Is (e.g. EOF).
			if emitErr := emit(ExecEvent{Type: "error", ExecID: execID, Code: protocol.ResultUnknown, Message: "未收到任务终态，执行结果未确认。"}); emitErr != nil {
				return emitErr
			}
			return fmt.Errorf("未收到任务终态，执行结果未确认：%w", err)
		}
		if fr.ID != requestID {
			return unknown("任务响应不匹配，执行结果未确认。")
		}
		if fr.ExecID != "" {
			if execID != "" && execID != fr.ExecID {
				return unknown("任务标识不匹配，执行结果未确认。")
			}
			execID = fr.ExecID
		}
		if fr.Event != "" {
			if fr.OK != nil || fr.Code != "" || len(fr.Result) != 0 || fr.Event != "stdout" && fr.Event != "stderr" {
				return unknown("任务事件无效，执行结果未确认。")
			}
			if err := emit(ExecEvent{Type: fr.Event, ExecID: execID, Data: fr.Data}); err != nil {
				return err
			}
			continue
		}
		if fr.OK == nil {
			return unknown("任务响应无效，执行结果未确认。")
		}
		if !*fr.OK {
			if fr.Code == "" {
				return unknown("任务错误缺少错误码，执行结果未确认。")
			}
			return fail(fr.Code, Human(fr.Code))
		}
		if fr.Code != "" {
			return unknown("任务响应矛盾，执行结果未确认。")
		}
		var result struct {
			Status     string `json:"status"`
			ExitCode   *int   `json:"exit_code"`
			DurationMS *int64 `json:"duration_ms"`
			Truncated  *bool  `json:"truncated"`
			ExecID     string `json:"exec_id"`
		}
		if json.Unmarshal(fr.Result, &result) != nil || result.DurationMS != nil && *result.DurationMS < 0 {
			return unknown("任务结果无效，执行结果未确认。")
		}
		if result.ExecID != "" {
			if execID != "" && execID != result.ExecID {
				return unknown("任务结果标识不匹配，执行结果未确认。")
			}
			execID = result.ExecID
		}
		switch result.Status {
		case protocol.ExitCompleted:
			if result.ExitCode == nil {
				return unknown("缺少退出码，执行结果未确认。")
			}
		case protocol.ExitCancelled, protocol.ExitTimeout:
		default:
			return unknown("缺少有效终态，执行结果未确认。")
		}
		if err := emit(ExecEvent{Type: "exit", ExecID: execID, Status: result.Status, ExitCode: result.ExitCode, DurationMS: result.DurationMS, Truncated: result.Truncated}); err != nil {
			return err
		}
		switch result.Status {
		case protocol.ExitCancelled:
			return fmt.Errorf("%s", Human(protocol.Cancelled))
		case protocol.ExitTimeout:
			return fmt.Errorf("%s", Human(protocol.ExecTimeout))
		}
		if *result.ExitCode != 0 {
			return fmt.Errorf("任务失败，退出码 %d。", *result.ExitCode)
		}
		return nil
	}
}
