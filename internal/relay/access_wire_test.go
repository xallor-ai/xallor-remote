package relay

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/coder/websocket/wsjson"
	"github.com/xallor-ai/xallor-remote/internal/protocol"
)

// Real TLS sockets must observe fail-closed routing before the uncertain result,
// then recover through persisted ownership rather than the former bearer cache.
func TestAccessWire_should_reloadCommittedOwnership_whenCommitResultLost(t *testing.T) {
	s := &uncertainAccessStore{SQLStore: openIdentityTestStore(t)}
	a := enrollment(t, s.SQLStore, "alice", "")
	if _, err := s.RegisterDevice(context.Background(), "device", protocol.SHA256Hex("secret"), true); err != nil {
		t.Fatal(err)
	}
	setTestAccess(t, s, "device", protocol.SHA256Hex("bearer"), true)
	h := NewHub(s)
	dctx, device, hello := identityWire(t, h, protocol.Message{Type: protocol.TypeHelloDevice, DeviceID: "device", Secret: "secret"})
	if hello.Type != protocol.TypeHelloOK {
		t.Fatal(hello.Code)
	}
	cctx, old, hello := identityWire(t, h, protocol.Message{Type: protocol.TypeHelloClient, TargetID: "device", Grant: "bearer"})
	if hello.Type != protocol.TypeHelloOK {
		t.Fatal(hello.Code)
	}
	if err := wsjson.Write(dctx, device, protocol.Message{Type: protocol.TypeAccessRequest, RequestID: "claim", AccountToken: a.Token, Payload: json.RawMessage(`{"action":"claim"}`)}); err != nil {
		t.Fatal(err)
	}
	var reply protocol.Message
	if err := wsjson.Read(dctx, device, &reply); err != nil || reply.Type != protocol.TypeAccessResult || reply.RequestID != "claim" || reply.Code != protocol.RelayError {
		t.Fatal("management result", reply.Type, reply.Code, err)
	}
	if h.DeviceOnline("device") {
		t.Fatal("error reply preceded routing quarantine")
	}
	if err := wsjson.Read(dctx, device, &reply); err == nil {
		t.Fatal("device connection was not closed")
	}
	if err := wsjson.Write(cctx, old, protocol.Message{Type: protocol.TypeInvoke, Op: protocol.OpInfo, ExecID: "offline"}); err != nil {
		t.Fatal(err)
	}
	if err := wsjson.Read(cctx, old, &reply); err != nil || reply.Code != protocol.DeviceOffline {
		t.Fatal("old route survived uncertainty", reply.Code, err)
	}
	fctx, fresh, hello := identityWire(t, h, protocol.Message{Type: protocol.TypeHelloDevice, DeviceID: "device", Secret: "secret"})
	if hello.Type != protocol.TypeHelloOK {
		t.Fatal(hello.Code)
	}
	if err := wsjson.Write(cctx, old, protocol.Message{Type: protocol.TypeInvoke, Op: protocol.OpInfo, ExecID: "stale"}); err != nil {
		t.Fatal(err)
	}
	if err := wsjson.Read(cctx, old, &reply); err != nil || reply.Code != protocol.Unauthorized {
		t.Fatal("reconnect revived bearer", reply.Code, err)
	}
	octx, owner, hello := identityWire(t, h, protocol.Message{Type: protocol.TypeHelloClient, TargetID: "device", AccountToken: a.Token})
	if hello.Type != protocol.TypeHelloOK {
		t.Fatal(hello.Code)
	}
	if err := wsjson.Write(octx, owner, protocol.Message{Type: protocol.TypeInvoke, Op: protocol.OpInfo, ExecID: "owner"}); err != nil {
		t.Fatal(err)
	}
	if err := wsjson.Read(fctx, fresh, &reply); err != nil || reply.Type != protocol.TypeInvoke || reply.Op != protocol.OpInfo || reply.AccountToken != "" {
		t.Fatal("owner dispatch", err)
	}
	if err := wsjson.Write(fctx, fresh, protocol.Message{Type: protocol.TypeExit, ExecID: reply.ExecID, ExitCode: protocol.Int(0), Status: protocol.ExitCompleted}); err != nil {
		t.Fatal(err)
	}
	if err := wsjson.Read(octx, owner, &reply); err != nil || reply.Type != protocol.TypeExit {
		t.Fatal("owner result", err)
	}
}
