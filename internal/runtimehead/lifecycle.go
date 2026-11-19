package runtimehead

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"time"

	"github.com/xallor-ai/xallor-remote/internal/ipc"
	"github.com/xallor-ai/xallor-remote/internal/protocol"
	"github.com/xallor-ai/xallor-remote/internal/runtimeowner"
)

// OperationError preserves the original ID after any uncertain dispatch/result;
// it is never an invitation to submit the command again under a different ID.
type OperationError struct {
	ID    string
	Cause error
}

func (e *OperationError) Error() string {
	return fmt.Sprintf("本机版本事务 %s 未完成确认：%v；请查询原事务，不要重复提交。", e.ID, e.Cause)
}
func (e *OperationError) Unwrap() error { return e.Cause }

type OperationQuery func(context.Context, string) (runtimeowner.Record, error)

// SubmitLifecycle sends exactly once on the already admitted owner connection.
// Every follow-up is read-only, including after write/ack failure or replacement.
func SubmitLifecycle(ctx context.Context, raw net.Conn, conn *ipc.Conn, command runtimeowner.Command, query OperationQuery) (runtimeowner.Record, error) {
	request, err := command.Request()
	if err != nil {
		return runtimeowner.Record{}, err
	}
	if query == nil {
		return runtimeowner.Record{}, runtimeowner.ErrInvalid
	}
	namespace, err := lifecycleNamespace()
	if err != nil {
		return runtimeowner.Record{}, err
	}
	stop := context.AfterFunc(ctx, func() { _ = raw.Close() })
	defer stop()
	deadline := time.Now().Add(80 * time.Second)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	if err := raw.SetDeadline(deadline); err != nil {
		return runtimeowner.Record{}, &OperationError{ID: command.ID, Cause: err}
	}
	if err := ctx.Err(); err != nil {
		return runtimeowner.Record{}, &OperationError{ID: command.ID, Cause: err}
	}
	params := map[string]string{"operation_id": command.ID, "namespace_sha256": namespace}
	if command.Kind == "install" {
		params["bundle"] = command.Bundle
	}
	const id = "lifecycle-submit"
	deliveryErr := ipc.Call(conn, id, "runtime."+command.Kind, params)
	if deliveryErr == nil {
		frame, readErr := conn.ReadFrame()
		deliveryErr = readErr
		if readErr == nil {
			if frame.ID == id && frame.Event == "" && frame.OK != nil && !*frame.OK {
				switch frame.Code {
				case protocol.RuntimeSourceUnverified, protocol.RuntimeProfileMismatch, protocol.RuntimePendingRecovery, protocol.RuntimeUnavailable, protocol.AgentError:
					return runtimeowner.Record{}, &OperationError{ID: command.ID, Cause: &Refusal{Code: frame.Code, Message: ipc.FailureMessage(frame)}}
				}
			}
			var ack struct {
				Accepted bool   `json:"accepted"`
				ID       string `json:"operation_id"`
			}
			if frame.ID != id || frame.Event != "" || frame.OK == nil || !*frame.OK || frame.Code != "" || json.Unmarshal(frame.Result, &ack) != nil || !ack.Accepted || ack.ID != command.ID {
				deliveryErr = errors.New("事务接收回执未确认")
			}
		}
	}
	_ = raw.Close()
	last := deliveryErr
	for {
		if err := ctx.Err(); err != nil {
			return runtimeowner.Record{}, &OperationError{ID: command.ID, Cause: errors.Join(err, last)}
		}
		record, err := query(ctx, command.ID)
		if err == nil {
			if err := record.ValidateFor(request); err != nil {
				return record, &OperationError{ID: command.ID, Cause: err}
			}
			if err := record.ValidateNamespace(namespace); err != nil {
				return record, &OperationError{ID: command.ID, Cause: err}
			}
			switch record.Outcome {
			case runtimeowner.Succeeded:
				return record, nil
			case runtimeowner.Rejected, runtimeowner.Failed:
				return record, fmt.Errorf("本机版本事务 %s 已结束，结果为 %s；未报告安装成功。", command.ID, record.Outcome)
			case runtimeowner.Uncertain:
				return record, &OperationError{ID: command.ID, Cause: runtimeowner.ErrUncertain}
			}
		} else {
			last = err
		}
		timer := time.NewTimer(100 * time.Millisecond)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
		case <-timer.C:
		}
	}
}
