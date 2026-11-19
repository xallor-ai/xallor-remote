package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/xallor-ai/xallor-remote/internal/execution"
	"github.com/xallor-ai/xallor-remote/internal/ipc"
	"github.com/xallor-ai/xallor-remote/internal/protocol"
)

// A new IPC connection may belong to an unready replacement owner. Neither
// granting access nor executing a task may precede its readiness response.
func TestHead_should_notDispatchBusiness_whenConnectedOwnerRefuses(t *testing.T) {
	for _, method := range []string{"grant.issue", "exec", "exec-command"} {
		t.Run(method, func(t *testing.T) {
			t.Setenv("XALLOR_REMOTE_DATA_DIR", t.TempDir())
			ln, err := ipc.Listen()
			if err != nil {
				t.Fatal(err)
			}
			defer ln.Close()
			done := make(chan error, 1)
			go func() {
				raw, err := ln.Accept()
				if err != nil {
					done <- err
					return
				}
				defer raw.Close()
				_ = raw.SetDeadline(time.Now().Add(3 * time.Second))
				c := ipc.NewConn(raw)
				req, err := c.ReadRequest()
				if err != nil {
					done <- err
					return
				}
				if req.Method != "runtime.ensure" {
					_ = c.WriteFrame(ipc.OK(req.ID, map[string]any{"status": "completed", "exit_code": 0}))
					done <- fmt.Errorf("business dispatched before owner readiness: %s", req.Method)
					return
				}
				if err := c.WriteFrame(ipc.Fail(req.ID, protocol.RuntimePendingRecovery, "recovery required")); err != nil {
					done <- err
					return
				}
				if req, err = c.ReadRequest(); err == nil {
					done <- fmt.Errorf("request after owner refusal: %s", req.Method)
					return
				} else if e, ok := err.(interface{ Timeout() bool }); ok && e.Timeout() {
					done <- fmt.Errorf("refused connection was not closed: %w", err)
					return
				}
				done <- nil
			}()
			var callErr error
			var out bytes.Buffer
			if method == "exec-command" {
				cmd := cmdExec()
				cmd.SilenceUsage, cmd.SilenceErrors = true, true
				cmd.SetOut(&out)
				cmd.SetErr(io.Discard)
				cmd.SetArgs([]string{"--device", "peer", "--jsonl", "--", "echo probe"})
				callErr = cmd.Execute()
			} else if method == "exec" {
				callErr = streamExec("peer", "", execution.Request{Script: &execution.Script{Text: "echo probe"}}, true, &out, io.Discard)
			} else {
				_, callErr = rpc(method, map[string]any{})
			}
			serverErr := <-done
			if serverErr != nil {
				t.Error(serverErr)
			}
			if callErr == nil || !strings.Contains(callErr.Error(), "recovery required") {
				t.Fatalf("owner refusal lost: %v", callErr)
			}
			if method == "exec" || method == "exec-command" {
				var ev ipc.ExecEvent
				if err := json.Unmarshal(out.Bytes(), &ev); err != nil || ev.Type != "error" || ev.Code != protocol.RuntimePendingRecovery {
					t.Fatalf("incorrect pre-dispatch JSONL: %s (%v)", out.String(), err)
				}
			}
		})
	}
}
