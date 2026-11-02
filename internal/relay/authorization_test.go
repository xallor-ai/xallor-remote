package relay

import (
	"github.com/xallor-ai/xallor-remote/internal/protocol"
	"testing"
)

// An already authenticated connection must lose permission when its grant is rotated.
func TestInvoke_should_rejectExistingClient_whenGrantRotated(t *testing.T) {
	h := NewHub(newMemStore())
	dev, client := &recSender{}, &recSender{}
	h.RegisterDevice("123456789", "secret", true, dev)
	h.Handle(dev, protocol.RoleDevice, "123456789", protocol.Message{Type: protocol.TypeGrantRotate, NewGrantHash: protocol.SHA256Hex("old")})
	if code := h.RegisterClient("123456789", "old", "", client); code != "" {
		t.Fatal(code)
	}
	h.Handle(dev, protocol.RoleDevice, "123456789", protocol.Message{Type: protocol.TypeGrantRotate, NewGrantHash: protocol.SHA256Hex("new")})
	h.Handle(client, protocol.RoleClient, "123456789", protocol.Message{Type: protocol.TypeInvoke, Op: protocol.OpInfo, ExecID: "after-rotation"})
	if client.last().Code != protocol.Unauthorized {
		t.Fatalf("old connection retained permission: %+v", client.last())
	}
	if h.requests[requestKey{client: client, id: "after-rotation"}] != "" {
		t.Fatal("unauthorized invoke reached device")
	}
}

// Device responses must originate from the current connection for that inflight's target.
func TestForward_should_ignoreOtherDevice_and_keepOriginalTask(t *testing.T) {
	h := NewHub(newMemStore())
	a, b, client := &recSender{}, &recSender{}, &recSender{}
	h.RegisterDevice("dev_a", "secret-a", true, a)
	h.RegisterDevice("dev_b", "secret-b", true, b)
	authorizeTestClient(t, h, a, client)
	h.Handle(client, protocol.RoleClient, "dev_a", protocol.Message{Type: protocol.TypeInvoke, Op: protocol.OpInfo, ExecID: "owned"})
	zero := 0
	forged := protocol.Message{Type: protocol.TypeExit, ExecID: a.last().ExecID, ExitCode: &zero, Status: protocol.ExitCompleted}
	h.Handle(b, protocol.RoleDevice, "dev_b", forged)
	if client.last().Type == protocol.TypeExit {
		t.Fatal("other device forged terminal result")
	}
	h.Handle(a, protocol.RoleDevice, "dev_a", forged)
	if client.last().Type != protocol.TypeExit {
		t.Fatal("valid terminal was lost")
	}
}

// Client bindings cannot be reused after drop or used to address a different target.
func TestInvoke_should_requireLiveMatchingBinding(t *testing.T) {
	h := NewHub(newMemStore())
	dev, client := &recSender{}, &recSender{}
	h.RegisterDevice("dev_a", "secret", true, dev)
	authorizeTestClient(t, h, dev, client)
	h.Handle(client, protocol.RoleClient, "other", protocol.Message{Type: protocol.TypeInvoke, Op: protocol.OpInfo, ExecID: "wrong-target"})
	if client.last().Code != protocol.Unauthorized {
		t.Fatal("accepted wrong target binding")
	}
	h.Drop(client, protocol.RoleClient, "dev_a")
	h.Handle(client, protocol.RoleClient, "dev_a", protocol.Message{Type: protocol.TypeInvoke, Op: protocol.OpInfo, ExecID: "after-drop"})
	if client.last().Code != protocol.Unauthorized || len(h.clients) != 0 {
		t.Fatal("retained dropped client binding")
	}
}
