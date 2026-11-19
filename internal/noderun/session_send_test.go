package noderun

import (
	"context"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/xallor-ai/xallor-remote/internal/identity"
	"github.com/xallor-ai/xallor-remote/internal/ipc"
	"github.com/xallor-ai/xallor-remote/internal/protocol"
)

// The transport API does not report whether a failed write reached the peer.
// Both session operations must conservatively expose unknown, except a local
// size refusal which is guaranteed to precede transport write admission.
func TestSessionIPC_shouldPreserveUnknown_whenTransportSendFails(t *testing.T) {
	for _, method := range []string{"session.request", "session.resume"} {
		for _, large := range []bool{false, true} {
			name := method + "/transport-failure"
			if large {
				name = method + "/too-large"
			}
			t.Run(name, func(t *testing.T) {
				store, err := identity.LoadFrom(t.TempDir())
				if err != nil {
					t.Fatal(err)
				}
				const target = "123456789"
				if err := store.AddPeer(target, "grant"); err != nil {
					t.Fatal(err)
				}
				d := New(store, nil)
				defer d.Stop()
				relay, token, err := store.Credentials()
				if err != nil {
					t.Fatal(err)
				}
				ctx, cancel := context.WithCancel(d.ctx)
				cancel()
				link := &wsConn{ctx: ctx}
				d.clients[clientKey{relay: relay, target: target, grantHash: protocol.SHA256Hex("grant"), credentialHash: protocol.SHA256Hex(token)}] = &clientLink{ws: link}
				secret := "capability"
				want := protocol.SessionUnknown
				if large {
					secret = strings.Repeat("x", protocol.MaxWSMessageBytes+1)
					want = protocol.TooLarge
				}
				a, b := net.Pipe()
				defer a.Close()
				defer b.Close()
				_ = b.SetDeadline(time.Now().Add(3 * time.Second))
				done := make(chan struct{})
				go func() {
					defer close(done)
					req := ipc.Request{ID: "ipc", Method: method}
					if method == "session.resume" {
						req.Params = mustJSON(sessionResumeParams{DeviceID: target, SessionID: "session", ResumeID: "resume", ResumeCapability: secret, TargetUID: "uid"})
						d.ipcSessionResume(ipc.NewConn(a), req)
					} else {
						req.Params = mustJSON(sessionRequestParams{DeviceID: target, SessionID: "session", CodeID: "code", CodeSecret: secret, TargetUID: "uid", Generation: 1, Scope: []string{protocol.OpInfo}})
						d.ipcSessionRequest(ipc.NewConn(a), req)
					}
				}()
				got, err := ipc.NewConn(b).ReadFrame()
				if err != nil {
					t.Fatal(err)
				}
				<-done
				if got.OK == nil || *got.OK || got.Code != want {
					t.Fatalf("code=%s want=%s", got.Code, want)
				}
			})
		}
	}
}
