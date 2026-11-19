package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/xallor-ai/xallor-remote/internal/buildinfo"
	"github.com/xallor-ai/xallor-remote/internal/execution"
	"github.com/xallor-ai/xallor-remote/internal/ipc"
)

// A ready owner must receive the business request on the very same connection;
// stream output and its unique terminal must survive the readiness handshake.
func TestHead_should_dispatchOnSameConnection_whenOwnerIsReady(t *testing.T) {
	for _, method := range []string{"grant.issue", "exec", "status"} {
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
				if method != "status" {
					if req.Method != "runtime.ensure" {
						done <- fmt.Errorf("missing ensure: %s", req.Method)
						return
					}
					var params struct {
						Protocol int `json:"required_protocol"`
						Profile  int `json:"required_profile_format"`
					}
					if json.Unmarshal(req.Params, &params) != nil || params.Protocol != buildinfo.Protocol || params.Profile != buildinfo.ProfileFormat() {
						done <- fmt.Errorf("caller requirements missing: %s", req.Params)
						return
					}
					ready := map[string]any{"runtime": map[string]any{
						"state": "ready", "version": buildinfo.Version(), "revision": strings.Repeat("a", 40),
						"protocol": buildinfo.Protocol, "profile_format": buildinfo.ProfileFormat(), "pid": 123,
						"sha256": strings.Repeat("b", 64), "activation_id": strings.Repeat("c", 32), "launch_id": strings.Repeat("d", 32),
					}}
					if err := c.WriteFrame(ipc.OK(req.ID, ready)); err != nil {
						done <- err
						return
					}
					req, err = c.ReadRequest()
					if err != nil {
						done <- err
						return
					}
				}
				if req.Method != method {
					done <- fmt.Errorf("unexpected business method: %s", req.Method)
					return
				}
				if method == "exec" {
					if err := c.WriteFrame(ipc.Event(req.ID, "stdout", "probe")); err != nil {
						done <- err
						return
					}
					done <- c.WriteFrame(ipc.OK(req.ID, map[string]any{"status": "completed", "exit_code": 0}))
				} else {
					done <- c.WriteFrame(ipc.OK(req.ID, map[string]any{"observed": true}))
				}
			}()
			if method == "exec" {
				var output bytes.Buffer
				err := streamExec("peer", "", execution.Request{Script: &execution.Script{Text: "echo probe"}}, false, &output, io.Discard)
				if err != nil || output.String() != "probe" {
					t.Errorf("stream lost: %q %v", output.String(), err)
				}
			} else {
				result, err := rpc(method, nil)
				if err != nil || result["observed"] != true {
					t.Errorf("business response lost: %v %v", result, err)
				}
			}
			if err := <-done; err != nil {
				t.Fatal(err)
			}
		})
	}
}
