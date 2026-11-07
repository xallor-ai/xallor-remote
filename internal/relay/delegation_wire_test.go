package relay

import (
	"context"
	"strings"
	"testing"

	"github.com/coder/websocket/wsjson"
	"github.com/xallor-ai/xallor-remote/internal/access"
	"github.com/xallor-ai/xallor-remote/internal/account"
	"github.com/xallor-ai/xallor-remote/internal/admission"
	"github.com/xallor-ai/xallor-remote/internal/delegation"
	"github.com/xallor-ai/xallor-remote/internal/protocol"
)

// Real TLS routing replaces forged caller proofs, restricts renewal to the
// authenticated target, and revalidates credentials after the submit route ends.
func TestDelegation_should_bindProofToDeviceAndOriginalCredential_afterSubmitEnds(t *testing.T) {
	s := openIdentityTestStore(t)
	actor := enrollment(t, s, "alice", "")
	key, err := s.ExecutionKey()
	if err != nil {
		t.Fatal(err)
	}
	permissions := access.NewService(s)
	h := NewHub(s, s, permissions, delegation.NewAuthority(key, s, s, permissions), admission.New(admission.Limits{PerDevice: protocol.MaxDeviceRequests}))
	dctx, target, hello := identityWire(t, h, protocol.Message{Type: protocol.TypeHelloDevice, DeviceID: "device", Secret: "secret"})
	if hello.Type != protocol.TypeHelloOK {
		t.Fatal(hello)
	}
	confirmWireSettings(t, dctx, target, "grant", true)
	cctx, caller, hello := identityWire(t, h, protocol.Message{Type: protocol.TypeHelloClient, TargetID: "device", Grant: "grant", AccountToken: actor.Token})
	if hello.Type != protocol.TypeHelloOK {
		t.Fatal(hello)
	}
	if err = wsjson.Write(cctx, caller, protocol.Message{Type: protocol.TypeInvoke, Op: protocol.OpHandoffSubmit, ExecID: "submit", Delegation: "forged"}); err != nil {
		t.Fatal(err)
	}
	var invoke protocol.Message
	if err = wsjson.Read(dctx, target, &invoke); err != nil {
		t.Fatal(err)
	}
	if invoke.Delegation == "" || invoke.Delegation == "forged" || invoke.Scope == nil {
		t.Fatal("no authority proof")
	}
	if _, err := h.executionAuthority.Renew(context.Background(), "device", invoke.Scope.DeviceUID, invoke.Delegation); err != nil {
		t.Fatal("direct verification", err)
	}
	if err = wsjson.Write(dctx, target, protocol.Message{Type: protocol.TypeExit, ExecID: invoke.ExecID, ExitCode: protocol.Int(0)}); err != nil {
		t.Fatal(err)
	}
	var reply protocol.Message
	if err = wsjson.Read(cctx, caller, &reply); err != nil {
		t.Fatal(err)
	}
	request := protocol.Message{Type: protocol.TypeExecutionRenew, RequestID: "renew", Delegation: invoke.Delegation}
	if err = wsjson.Write(cctx, caller, request); err != nil {
		t.Fatal(err)
	}
	if err = wsjson.Read(cctx, caller, &reply); err != nil || reply.Code != protocol.Unauthorized {
		t.Fatal("controller renewed", err)
	}
	otherCtx, other, hello := identityWire(t, h, protocol.Message{Type: protocol.TypeHelloDevice, DeviceID: "other", Secret: "other-secret"})
	if hello.Type != protocol.TypeHelloOK {
		t.Fatal(hello)
	}
	confirmWireSettings(t, otherCtx, other, "other-grant", true)
	if err = wsjson.Write(otherCtx, other, request); err != nil {
		t.Fatal(err)
	}
	if err = wsjson.Read(otherCtx, other, &reply); err != nil || reply.Code != protocol.Unauthorized {
		t.Fatal("other device renewed", err)
	}
	renew := func(want string) {
		t.Helper()
		reply = protocol.Message{}
		if err := wsjson.Write(dctx, target, request); err != nil {
			t.Fatal(err)
		}
		if err := wsjson.Read(dctx, target, &reply); err != nil || reply.Code != want || reply.Type != protocol.TypeExecutionLease {
			t.Fatal("renewal", err, reply.Code)
		}
	}
	renew("")
	if reply.LeaseMS <= 0 || reply.LeaseMS > protocol.ExecutionLeaseDuration.Milliseconds() {
		t.Fatal("invalid duration")
	}
	if err = account.New(s).RevokeCredential(context.Background(), actor.CredentialID); err != nil {
		t.Fatal(err)
	}
	renew(protocol.Unauthorized)
	request.Delegation = "A" + request.Delegation[1:]
	renew(protocol.Unauthorized)
}

// Unused or forged proof text still consumes the bounded connection inbox.
func TestDelegation_should_chargeProofBytes_beforeAuthorization(t *testing.T) {
	plain := protocol.Message{}
	forged := protocol.Message{Delegation: strings.Repeat("x", 10000)}
	if retainedBytes(forged)-retainedBytes(plain) != 10000 {
		t.Fatal("proof bypasses byte budget")
	}
}
