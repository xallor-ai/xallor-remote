package noderun

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/xallor-ai/xallor-remote/internal/identity"
	"github.com/xallor-ai/xallor-remote/internal/ipc"
	"github.com/xallor-ai/xallor-remote/internal/protocol"
)

// Use real IPC handlers and a shared WebSocket, with an explicit Relay peer
// simulator controlling response order. This tests correlation, not redemption.
func TestSessionIPC_shouldKeepCallerResultsSeparate_whenSameSessionRepliesReverse(t *testing.T) {
	for _, method := range []string{"session.request", "session.resume"} {
		t.Run(method, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
			defer cancel()
			firstReceived := make(chan struct{})
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				conn, err := websocket.Accept(w, r, nil)
				if err != nil {
					t.Error(err)
					return
				}
				defer conn.CloseNow()
				var hello protocol.Message
				if err := wsjson.Read(ctx, conn, &hello); err != nil {
					t.Error(err)
					return
				}
				if err := wsjson.Write(ctx, conn, protocol.Message{Type: protocol.TypeHelloOK, Role: protocol.RoleClient, ClientAuth: &protocol.ClientAuthentication{Mode: protocol.ClientAuthShared}}); err != nil {
					t.Error(err)
					return
				}
				messages := make([]protocol.Message, 2)
				for i := range messages {
					if err := wsjson.Read(ctx, conn, &messages[i]); err != nil {
						t.Error(err)
						return
					}
					if i == 0 {
						close(firstReceived)
					}
				}
				if messages[0].SessionID != messages[1].SessionID || messages[0].RequestID == messages[1].RequestID {
					t.Error("fixture did not produce two independent requests for one session")
					return
				}
				for _, i := range []int{1, 0} {
					resultType := protocol.TypeSessionResult
					if method == "session.resume" {
						resultType = protocol.TypeSessionResumeResult
					}
					result := protocol.Message{Type: resultType, RequestID: messages[i].RequestID, SessionID: messages[i].SessionID, Status: protocol.SessionActive, ResumeID: "rotated-a", ResumeCapability: "only-a"}
					if i == 1 {
						result.Status = protocol.SessionRejectedState
						result.Code = protocol.Unauthorized
						result.ResumeID = ""
						result.ResumeCapability = ""
					}
					if err := wsjson.Write(ctx, conn, result); err != nil {
						t.Error(err)
						return
					}
				}
				// Keep the peer alive until both callers consumed their own result.
				<-ctx.Done()
			}))
			defer server.Close()
			defer cancel()
			store, err := identity.LoadFrom(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			const target = "123456789"
			if err := store.SetConfig("ws"+strings.TrimPrefix(server.URL, "http"), "", ""); err != nil {
				t.Fatal(err)
			}
			if err := store.AddPeer(target, "grant"); err != nil {
				t.Fatal(err)
			}
			d := New(store, nil)
			defer d.Stop()
			start := func(id string) <-chan ipc.Frame {
				a, b := net.Pipe()
				t.Cleanup(func() { _ = a.Close(); _ = b.Close() })
				_ = a.SetDeadline(time.Now().Add(5 * time.Second))
				_ = b.SetDeadline(time.Now().Add(5 * time.Second))
				frames := make(chan ipc.Frame, 1)
				done := make(chan struct{})
				go func() {
					defer close(done)
					req := ipc.Request{ID: id, Method: method}
					if method == "session.resume" {
						req.Params = mustJSON(sessionResumeParams{DeviceID: target, SessionID: "same", ResumeID: "proof", ResumeCapability: "cap", TargetUID: "uid"})
						d.ipcSessionResume(ipc.NewConn(a), req)
					} else {
						req.Params = mustJSON(sessionRequestParams{DeviceID: target, SessionID: "same", CodeID: "code", CodeSecret: "secret", TargetUID: "uid", Generation: 1, Scope: []string{protocol.OpInfo}})
						d.ipcSessionRequest(ipc.NewConn(a), req)
					}
				}()
				go func() {
					f, e := ipc.NewConn(b).ReadFrame()
					if e != nil {
						f = ipc.Frame{Code: "read_failed"}
					}
					frames <- f
				}()
				t.Cleanup(func() {
					d.Stop()
					_ = a.Close()
					_ = b.Close()
					select {
					case <-done:
					case <-time.After(time.Second):
						t.Error("IPC handler did not finish")
					}
				})
				return frames
			}
			a := start("a")
			select {
			case <-firstReceived:
			case <-ctx.Done():
				t.Fatal("first request did not reach peer")
			}
			b := start("b")
			for _, tc := range []struct {
				id string
				ch <-chan ipc.Frame
				ok bool
			}{{"a", a, true}, {"b", b, false}} {
				select {
				case result := <-tc.ch:
					if result.ID != tc.id || result.OK == nil || *result.OK != tc.ok {
						t.Fatalf("caller %s received wrong result: code=%s", tc.id, result.Code)
					}
					if tc.ok {
						var payload struct {
							ResumeCapability string `json:"resume_capability"`
						}
						if json.Unmarshal(result.Result, &payload) != nil || payload.ResumeCapability != "only-a" {
							t.Fatal("caller a lost its capability")
						}
					} else if result.Code != protocol.Unauthorized || len(result.Result) != 0 {
						t.Fatal("caller b received wrong terminal or capability")
					}
				case <-ctx.Done():
					t.Fatal("caller result never arrived")
				}
			}
		})
	}
}
