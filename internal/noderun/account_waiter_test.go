package noderun

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/xallor-ai/xallor-remote/internal/identity"
	"github.com/xallor-ai/xallor-remote/internal/protocol"
)

// Done is observed only after the first dial is blocked. It synchronizes the
// second caller's wait-select without sleep-based scheduling or production hooks.
type observedWaitContext struct {
	context.Context
	armed   atomic.Bool
	once    sync.Once
	waiting chan struct{}
}

func (c *observedWaitContext) Done() <-chan struct{} {
	if c.armed.Load() {
		c.once.Do(func() { close(c.waiting) })
	}
	return c.Context.Done()
}

// Waiting callers must retain their original identity generation. Clearing or
// switching accounts cannot redispatch an already-pending request as another actor.
func TestAccount_should_rejectWaitingCaller_whenIdentityChanges(t *testing.T) {
	for _, change := range []string{"clear", "switch", "switch_back"} {
		t.Run(change, func(t *testing.T) {
			entered, release := make(chan struct{}), make(chan struct{})
			var calls atomic.Int32
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				c, err := websocket.Accept(w, r, nil)
				if err != nil {
					return
				}
				defer c.CloseNow()
				var hello protocol.Message
				if wsjson.Read(ctx, c, &hello) != nil {
					return
				}
				if calls.Add(1) == 1 {
					close(entered)
					select {
					case <-release:
					case <-ctx.Done():
						return
					}
				}
				mode := protocol.ClientAuthShared
				if hello.AccountToken != "" {
					mode = protocol.ClientAuthAccount
				}
				if wsjson.Write(ctx, c, protocol.Message{Type: protocol.TypeHelloOK, Role: protocol.RoleClient, ClientAuth: &protocol.ClientAuthentication{Mode: mode}}) != nil {
					return
				}
				var next protocol.Message
				_ = wsjson.Read(ctx, c, &next)
			}))
			defer srv.Close()
			store, err := identity.LoadFrom(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			if err := store.SetConfig("ws"+strings.TrimPrefix(srv.URL, "http"), "", ""); err != nil {
				t.Fatal(err)
			}
			d := New(store, nil)
			defer d.Stop()
			observed := &observedWaitContext{Context: d.ctx, waiting: make(chan struct{})}
			d.ctx = observed
			a, b := "xr_account_"+strings.Repeat("a", 64), "xr_account_"+strings.Repeat("b", 64)
			if err := d.setAccount(a); err != nil {
				t.Fatal(err)
			}
			peer := identity.Peer{DeviceID: "123456789", Grant: "grant"}
			results := make(chan error, 2)
			start := func() { _, _, err := d.ensureClient(peer); results <- err }
			go start()
			select {
			case <-entered:
			case <-time.After(5 * time.Second):
				t.Fatal("first dial did not start")
			}
			observed.armed.Store(true)
			go start()
			select {
			case <-observed.waiting:
			case <-time.After(5 * time.Second):
				t.Fatal("second caller did not wait on the original dial")
			}
			token := b
			if change == "clear" {
				token = ""
			}
			if err := d.setAccount(token); err != nil {
				t.Fatal(err)
			}
			if change == "switch_back" {
				if err := d.setAccount(a); err != nil {
					t.Fatal(err)
				}
			}
			close(release)
			for i := 0; i < 2; i++ {
				select {
				case err := <-results:
					if err == nil {
						t.Error("pending caller adopted replacement identity")
					}
				case <-time.After(5 * time.Second):
					t.Fatal("caller did not settle")
				}
			}
			if calls.Load() != 1 {
				t.Error("pending caller retried with a different identity")
			}
		})
	}
}
