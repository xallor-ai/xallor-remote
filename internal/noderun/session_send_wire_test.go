package noderun

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/xallor-ai/xallor-remote/internal/identity"
	"github.com/xallor-ai/xallor-remote/internal/ipc"
	"github.com/xallor-ai/xallor-remote/internal/protocol"
)

type sessionDeliveredWriteError struct {
	net.Conn
	armed    atomic.Bool
	ctx      context.Context
	received <-chan struct{}
}

func (c *sessionDeliveredWriteError) Write(data []byte) (int, error) {
	n, err := c.Conn.Write(data)
	if err == nil && c.armed.CompareAndSwap(true, false) {
		select {
		case <-c.received:
			return n, io.ErrUnexpectedEOF
		case <-c.ctx.Done():
			return n, c.ctx.Err()
		}
	}
	return n, err
}

// Inject a local write error only after the real WebSocket peer has decoded the
// entire session mutation. Receipt proves possible execution, not SQL commit;
// IPC must return unknown and must neither replay nor claim deterministic denial.
func TestSessionIPC_shouldReturnUnknown_whenPeerReceivedFailedWrite(t *testing.T) {
	for _, method := range []string{"session.request", "session.resume"} {
		t.Run(method, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			received := make(chan struct{})
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				c, err := websocket.Accept(w, r, nil)
				if err != nil {
					t.Error(err)
					return
				}
				defer c.CloseNow()
				var m protocol.Message
				if err := wsjson.Read(ctx, c, &m); err != nil {
					t.Error(err)
					return
				}
				if m.RequestID == "" || m.SessionID != "session" {
					t.Error("peer did not receive complete mutation")
					return
				}
				close(received)
				<-ctx.Done()
			}))
			defer server.Close()
			defer cancel()
			var raw *sessionDeliveredWriteError
			transport := &http.Transport{DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
				c, err := (&net.Dialer{}).DialContext(ctx, network, address)
				if err != nil {
					return nil, err
				}
				raw = &sessionDeliveredWriteError{Conn: c, ctx: ctx, received: received}
				return raw, nil
			}}
			defer transport.CloseIdleConnections()
			url := "ws" + strings.TrimPrefix(server.URL, "http")
			conn, _, err := websocket.Dial(ctx, url, &websocket.DialOptions{HTTPClient: &http.Client{Transport: transport}})
			if err != nil {
				t.Fatal(err)
			}
			defer conn.CloseNow()
			// net/http can finish its dialing context after the upgrade. Keep
			// fault observation bounded by the test context, not that dial.
			raw.ctx = ctx
			raw.armed.Store(true)
			store, err := identity.LoadFrom(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			const target = "123456789"
			if err := store.SetConfig(url, "", ""); err != nil {
				t.Fatal(err)
			}
			if err := store.AddPeer(target, "grant"); err != nil {
				t.Fatal(err)
			}
			d := New(store, nil)
			defer d.Stop()
			_, token, err := store.Credentials()
			if err != nil {
				t.Fatal(err)
			}
			// Inject at the already-authenticated transport boundary. No Relay
			// state machine or source authentication is claimed by this fixture.
			link := &wsConn{ctx: ctx, conn: conn}
			d.clients[clientKey{relay: url, target: target, grantHash: protocol.SHA256Hex("grant"), credentialHash: protocol.SHA256Hex(token)}] = &clientLink{ws: link}
			a, b := net.Pipe()
			defer a.Close()
			defer b.Close()
			_ = a.SetDeadline(time.Now().Add(5 * time.Second))
			_ = b.SetDeadline(time.Now().Add(5 * time.Second))
			done := make(chan struct{})
			go func() {
				defer close(done)
				req := ipc.Request{ID: "ipc", Method: method}
				if method == "session.resume" {
					req.Params = mustJSON(sessionResumeParams{DeviceID: target, SessionID: "session", ResumeID: "resume", ResumeCapability: "capability", TargetUID: "uid"})
					d.ipcSessionResume(ipc.NewConn(a), req)
				} else {
					req.Params = mustJSON(sessionRequestParams{DeviceID: target, SessionID: "session", CodeID: "code", CodeSecret: "secret", TargetUID: "uid", Generation: 1, Scope: []string{protocol.OpInfo}})
					d.ipcSessionRequest(ipc.NewConn(a), req)
				}
			}()
			f, err := ipc.NewConn(b).ReadFrame()
			if err != nil {
				t.Fatal(err)
			}
			<-done
			select {
			case <-received:
			default:
				t.Fatal("fault did not follow confirmed peer receipt")
			}
			if f.OK == nil || *f.OK || f.Code != protocol.SessionUnknown {
				t.Fatalf("uncertain send misclassified: %s", f.Code)
			}
		})
	}
}
