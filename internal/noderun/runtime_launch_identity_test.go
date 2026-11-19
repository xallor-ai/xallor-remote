package noderun

import (
	"context"
	"encoding/json"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/xallor-ai/xallor-remote/internal/ipc"
	"github.com/xallor-ai/xallor-remote/internal/protocol"
	"github.com/xallor-ai/xallor-remote/internal/release"
)

// Source selection cannot supply a replacement process's launch identity.
// Missing, malformed or mismatched process-bound IDs deny ensure and are not
// exposed as a confirmed launch in status, even with otherwise ready metadata.
func TestOwnerObservation_should_requireProcessBoundLaunchIdentity(t *testing.T) {
	id := strings.Repeat("a", 32)
	for _, tc := range []struct {
		name, processID, selectedID string
		ready                       bool
	}{{"matching", id, id, true}, {"changed", id, strings.Repeat("b", 32), false},
		{"unbound-process", "", id, false}, {"missing-selection", id, "", false},
		{"both-empty", "", "", false}, {"malformed", "short", "short", false}} {
		for _, method := range []string{"runtime.status", "runtime.ensure"} {
			t.Run(tc.name+"/"+method, func(t *testing.T) {
				d := New(ownerTestStore(t), nil).WithLaunchIdentity(tc.processID)
				defer d.Stop()
				a, b := net.Pipe()
				defer a.Close()
				defer b.Close()
				_ = b.SetDeadline(time.Now().Add(3 * time.Second))
				d.startOwnerObservation(ipc.NewConn(a), ipc.Request{ID: "identity", Method: method},
					func(context.Context) (release.RuntimeObservation, error) {
						return release.RuntimeObservation{State: "ready", ActivationID: strings.Repeat("c", 32), LaunchID: tc.selectedID}, nil
					})
				frame, err := ipc.NewConn(b).ReadFrame()
				if err != nil || frame.OK == nil {
					t.Fatal(frame, err)
				}
				if method == "runtime.ensure" && !tc.ready {
					if *frame.OK || frame.Code != protocol.RuntimeStartFailed {
						t.Fatal("mismatched launch admitted", frame)
					}
				} else {
					var result struct {
						Runtime release.RuntimeObservation `json:"runtime"`
					}
					if !*frame.OK || json.Unmarshal(frame.Result, &result) != nil {
						t.Fatal(frame)
					}
					if tc.ready {
						if result.Runtime.State != "ready" || result.Runtime.LaunchID != id {
							t.Fatal(result)
						}
					} else if result.Runtime.State != "unknown" || result.Runtime.Code != protocol.RuntimeStartFailed || result.Runtime.LaunchID != "" || result.Runtime.ActivationID != "" {
						t.Fatal("status invented a confirmed launch", result)
					}
				}
				d.waitOwnerObservations()
			})
		}
	}
}
