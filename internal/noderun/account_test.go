package noderun

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/xallor-ai/xallor-remote/internal/identity"
	"github.com/xallor-ai/xallor-remote/internal/principal"
	"github.com/xallor-ai/xallor-remote/internal/protocol"
)

// A handshake already in flight cannot publish a connection after switching
// identity, including an A->B->A change before the server's response arrives.
func TestAccount_should_discardPendingConnection_whenIdentitySwitches(t *testing.T) {
	entered, release := make(chan string, 1), make(chan struct{})
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
		entered <- hello.AccountToken
		select {
		case <-release:
		case <-ctx.Done():
			return
		}
		if wsjson.Write(ctx, c, protocol.Message{Type: protocol.TypeHelloOK, Role: protocol.RoleClient}) != nil {
			return
		}
		var m protocol.Message
		_ = wsjson.Read(ctx, c, &m)
	}))
	defer srv.Close()
	s, err := identity.LoadFrom(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetConfig("ws"+strings.TrimPrefix(srv.URL, "http"), "", ""); err != nil {
		t.Fatal(err)
	}
	a, _ := principal.NewToken()
	b, _ := principal.NewToken()
	d := New(s, nil)
	defer d.Stop()
	if err := d.setAccount(a); err != nil {
		t.Fatal(err)
	}
	finished := make(chan error, 1)
	go func() {
		_, _, err := d.ensureClient(identity.Peer{DeviceID: "123456789", Grant: "grant"})
		finished <- err
	}()
	select {
	case token := <-entered:
		if token != a {
			t.Fatal("wrong identity used")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("handshake not started")
	}
	if err := d.setAccount(b); err != nil {
		t.Fatal(err)
	}
	if err := d.setAccount(a); err != nil {
		t.Fatal(err)
	}
	close(release)
	select {
	case err := <-finished:
		if err == nil {
			t.Fatal("stale connection published")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("pending connection did not settle")
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if len(d.clients) != 0 || len(d.ensuring) != 0 {
		t.Fatal("stale pooling state retained")
	}
}

// Account switching cancels old connection waiters and does not retain a pool
// entry whose authority/token scope belongs to the preceding profile identity.
func TestAccount_should_closeExistingLinks_whenCredentialChanged(t *testing.T) {
	s, err := identity.LoadFrom(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	d := New(s, nil)
	defer d.Stop()
	closed := false
	cl := &clientLink{cancel: func() { closed = true }, waiters: map[string]*execWaiter{}}
	w := cl.subscribe("old")
	d.clients[clientKey{target: "device", credentialHash: "old"}] = cl
	token, _ := principal.NewToken()
	if err := d.setAccount(token); err != nil {
		t.Fatal(err)
	}
	if !closed || len(d.clients) != 0 {
		t.Fatal("old links survived")
	}
	m, ok := w.next()
	if !ok || m.Type != protocol.TypeError {
		t.Fatal("old waiter was stranded")
	}
}
