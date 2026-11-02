package relay

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/xallor-ai/xallor-remote/internal/access"
	"github.com/xallor-ai/xallor-remote/internal/principal"
	"github.com/xallor-ai/xallor-remote/internal/protocol"
)

type accessCommitStore struct {
	*SQLStore
	afterCommit func()
}

type uncertainAccessStore struct{ *SQLStore }

func (s *uncertainAccessStore) ManageAccess(ctx context.Context, id string, expected access.Device, actor principal.Identity, request access.Request) (access.Mutation, error) {
	_, err := s.SQLStore.ManageAccess(ctx, id, expected, actor, request)
	if err != nil {
		return access.Mutation{}, err
	}
	return access.Mutation{}, errors.New("fixture: committed transaction response lost")
}

// An operational failure can represent an uncertain committed claim. Existing
// anonymous clients must not continue routing under the old cached ownership.
func TestAccess_should_stopRouting_whenCommitOutcomeIsUncertain(t *testing.T) {
	s := &uncertainAccessStore{SQLStore: openIdentityTestStore(t)}
	a := enrollment(t, s.SQLStore, "alice", "")
	h := NewHub(s)
	device, client := &recSender{}, &recSender{}
	if code := h.RegisterDevice("device", "secret", true, device); code != "" {
		t.Fatal(code)
	}
	h.Handle(device, protocol.RoleDevice, "device", protocol.Message{Type: protocol.TypeDeviceSettings, RequestID: "settings", Inbound: protocol.Bool(true), GrantHash: protocol.SHA256Hex("bearer")})
	if code := h.RegisterClient("device", "bearer", "", client); code != "" {
		t.Fatal(code)
	}
	h.Handle(device, protocol.RoleDevice, "device", protocol.Message{Type: protocol.TypeAccessRequest, RequestID: "claim", AccountToken: a.Token, Payload: json.RawMessage(`{"action":"claim"}`)})
	if h.DeviceOnline("device") {
		t.Fatal("uncertain ownership write retained a routable connection")
	}
	h.Handle(client, protocol.RoleClient, "device", protocol.Message{Type: protocol.TypeInvoke, Op: protocol.OpInfo, ExecID: "unsafe"})
	if h.requests[requestKey{client: client, id: "unsafe"}] != "" {
		t.Fatal("old bearer used uncertain ownership")
	}
	if code := h.RegisterDevice("device", "secret", true, &recSender{}); code != "" {
		t.Fatal(code)
	}
	if code := h.RegisterClient("device", "bearer", "", &recSender{}); code != protocol.Unauthorized {
		t.Fatal("reconnect lost committed claim", code)
	}
}

func (s *accessCommitStore) ManageAccess(ctx context.Context, id string, expected access.Device, actor principal.Identity, request access.Request) (access.Mutation, error) {
	mutation, err := s.SQLStore.ManageAccess(ctx, id, expected, actor, request)
	if err == nil && s.afterCommit != nil {
		s.afterCommit()
	}
	return mutation, err
}

// Losing the device channel after SQL commit cannot leave anonymous access in
// the routing cache. Reconnection must retain the committed ownership identity.
func TestAccess_should_publishCommittedOwner_whenDeviceDisconnectsBeforeReply(t *testing.T) {
	s := &accessCommitStore{SQLStore: openIdentityTestStore(t)}
	a := enrollment(t, s.SQLStore, "alice", "")
	h := NewHub(s)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	device := &contextSender{ctx: ctx}
	if code := h.RegisterDevice("device", "secret", true, device); code != "" {
		t.Fatal(code)
	}
	h.Handle(device, protocol.RoleDevice, "device", protocol.Message{Type: protocol.TypeDeviceSettings, RequestID: "settings", Inbound: protocol.Bool(true), GrantHash: protocol.SHA256Hex("bearer")})
	if code := h.RegisterClient("device", "bearer", "", &recSender{}); code != "" {
		t.Fatal(code)
	}
	s.afterCommit = func() { cancel(); h.Drop(device, protocol.RoleDevice, "device") }
	h.Handle(device, protocol.RoleDevice, "device", protocol.Message{Type: protocol.TypeAccessRequest, RequestID: "claim", AccountToken: a.Token, Payload: json.RawMessage(`{"action":"claim"}`)})
	committed, err := s.LoadDevice(context.Background(), "device")
	if err != nil {
		t.Fatal(err)
	}
	if committed.OwnershipID == "" || h.devices["device"].record != committed || !h.access["device"].Matches(committed.Binding()) || len(h.clients) != 0 {
		t.Fatal("committed ownership not published")
	}
	if code := h.RegisterClient("device", "bearer", "", &recSender{}); code != protocol.Unauthorized {
		t.Fatal("offline ownership downgraded", code)
	}
	if code := h.RegisterDevice("device", "secret", true, &recSender{}); code != "" {
		t.Fatal(code)
	}
	if h.devices["device"].record.OwnershipID != committed.OwnershipID {
		t.Fatal("reconnect lost ownership")
	}
}
