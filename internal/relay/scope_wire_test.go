package relay

import (
	"context"
	"github.com/xallor-ai/xallor-remote/internal/access"
	"strings"
	"testing"

	"github.com/coder/websocket/wsjson"
	"github.com/xallor-ai/xallor-remote/internal/invocation"
	"github.com/xallor-ai/xallor-remote/internal/protocol"
)

// A real TLS connection must discard self-reported identity and credentials,
// deriving the target-side namespace solely from the authenticated client.
func TestScopeWire_should_replaceForgedIdentity_whenRoutingInvocation(t *testing.T) {
	s := openIdentityTestStore(t)
	a := enrollment(t, s, "alice", "")
	device, err := s.RegisterDevice(context.Background(), "device", protocol.SHA256Hex("secret"), true)
	if err != nil {
		t.Fatal(err)
	}
	device = setTestAccess(t, s, "device", protocol.SHA256Hex("bearer"), true)
	h := NewHub(s, s, access.NewService(s))
	dctx, target, hello := identityWire(t, h, protocol.Message{Type: protocol.TypeHelloDevice, DeviceID: "device", Secret: "secret"})
	if hello.Type != protocol.TypeHelloOK {
		t.Fatal(hello)
	}
	confirmWireSettings(t, dctx, target, "bearer", true)
	for _, token := range []string{"", a.Token} {
		cctx, caller, hello := identityWire(t, h, protocol.Message{Type: protocol.TypeHelloClient, TargetID: "device", Grant: "bearer", AccountToken: token})
		if hello.Type != protocol.TypeHelloOK {
			t.Fatal(hello)
		}
		forged := invocation.Scope{Authority: strings.Repeat("f", 32), DeviceUID: strings.Repeat("f", 32), GrantID: strings.Repeat("f", 32), PrincipalID: strings.Repeat("f", 32), TenantID: strings.Repeat("f", 32)}
		if err := wsjson.Write(cctx, caller, protocol.Message{Type: protocol.TypeInvoke, Op: protocol.OpHandoffGet, ExecID: "request", Scope: &forged, Secret: "must-not-forward", Grant: "must-not-forward", AccountToken: "must-not-forward"}); err != nil {
			t.Fatal(err)
		}
		var got protocol.Message
		if err := wsjson.Read(dctx, target, &got); err != nil {
			t.Fatal(err)
		}
		want := invocation.Scope{Authority: s.Authority(), DeviceUID: device.UID, GrantID: device.GrantID}
		if token != "" {
			want.PrincipalID = a.PrincipalID
			want.TenantID = a.TenantID
		}
		if got.Scope == nil || *got.Scope != want || !got.Scope.Valid() || got.Secret != "" || got.Grant != "" || got.AccountToken != "" {
			t.Fatal("untrusted context forwarded", got.Scope)
		}
		if err := wsjson.Write(dctx, target, protocol.Message{Type: protocol.TypeExit, ExecID: got.ExecID, ExitCode: protocol.Int(0)}); err != nil {
			t.Fatal(err)
		}
		if err := wsjson.Read(cctx, caller, &got); err != nil || got.Type != protocol.TypeExit {
			t.Fatal(err)
		}
	}
}

// Unknown scope strings still occupy memory before routing sanitizes them.
func TestScope_should_countRetainedIdentityBytes_whenEnvelopeIsUntrusted(t *testing.T) {
	base := protocol.Message{Type: protocol.TypeInvoke}
	with := base
	with.Scope = &invocation.Scope{Authority: strings.Repeat("x", 1000), PrincipalID: strings.Repeat("y", 2000)}
	if retainedBytes(with)-retainedBytes(base) != 3000 {
		t.Fatal("scope escaped outbox budget")
	}
}
