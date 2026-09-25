package relay

import (
	"strings"
	"sync"
	"testing"

	"github.com/xallor-ai/xallor-remote/internal/protocol"
)

type memStore struct {
	mu   sync.Mutex
	rows map[string]devRow
}

type devRow struct {
	secret, grant string
	inbound       bool
}

func newMemStore() *memStore {
	return &memStore{rows: map[string]devRow{}}
}

func (m *memStore) LoadDevice(id string) (string, string, bool, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.rows[id]
	return r.secret, r.grant, r.inbound, ok
}

func (m *memStore) SaveDevice(id, secretHash, grantHash string, inbound bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.rows[id] = devRow{secretHash, grantHash, inbound}
}

func (m *memStore) DeleteDevice(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.rows, id)
}

type recSender struct {
	mu  sync.Mutex
	got []protocol.Message
}

func (r *recSender) Send(m protocol.Message) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.got = append(r.got, m)
	return nil
}

func (r *recSender) Close() {}

func (r *recSender) last() protocol.Message {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.got) == 0 {
		return protocol.Message{}
	}
	return r.got[len(r.got)-1]
}

func TestInvokeRoutesAndExitDeletesInflight(t *testing.T) {
	h := NewHub(newMemStore())
	dev, cli := &recSender{}, &recSender{}
	if code := h.RegisterDevice("dev_a", "secret", true, dev); code != "" {
		t.Fatal(code)
	}
	grant := "xr_grant_test"
	h.mu.Lock()
	h.grants["dev_a"] = protocol.SHA256Hex(grant)
	h.devices["dev_a"].inbound = true
	h.mu.Unlock()
	if code := h.RegisterClient("dev_a", grant, cli); code != "" {
		t.Fatal(code)
	}

	h.Handle(cli, protocol.RoleClient, "dev_a", protocol.Message{
		Type: protocol.TypeInvoke, ExecID: "ex_1", Op: protocol.OpExec,
	})
	if dev.last().ExecID != "ex_1" {
		t.Fatalf("device did not get invoke: %+v", dev.last())
	}

	h.Handle(dev, protocol.RoleDevice, "dev_a", protocol.Message{
		Type: protocol.TypeStdout, ExecID: "ex_1", Data: "hi\n",
	})
	if cli.last().Data != "hi\n" {
		t.Fatalf("client stdout %+v", cli.last())
	}

	ec := 0
	h.Handle(dev, protocol.RoleDevice, "dev_a", protocol.Message{
		Type: protocol.TypeExit, ExecID: "ex_1", ExitCode: &ec, Status: protocol.ExitCompleted,
	})
	if cli.last().Type != protocol.TypeExit {
		t.Fatalf("want exit got %+v", cli.last())
	}

	h.Handle(dev, protocol.RoleDevice, "dev_a", protocol.Message{
		Type: protocol.TypeStdout, ExecID: "ex_1", Data: "late\n",
	})
	n := 0
	for _, m := range cli.got {
		if m.Type == protocol.TypeStdout {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("late stdout must drop, got %d stdout frames", n)
	}
}

func TestOversizeStdoutMarksTruncatedOnExit(t *testing.T) {
	h := NewHub(newMemStore())
	dev, cli := &recSender{}, &recSender{}
	h.RegisterDevice("dev_a", "secret", true, dev)
	h.mu.Lock()
	h.devices["dev_a"].inbound = true
	h.mu.Unlock()
	h.Handle(cli, protocol.RoleClient, "dev_a", protocol.Message{
		Type: protocol.TypeInvoke, ExecID: "ex_t", Op: protocol.OpExec,
	})
	big := strings.Repeat("x", protocol.MaxFrameBytes+1)
	h.Handle(dev, protocol.RoleDevice, "dev_a", protocol.Message{
		Type: protocol.TypeStdout, ExecID: "ex_t", Data: big,
	})
	ec := 0
	h.Handle(dev, protocol.RoleDevice, "dev_a", protocol.Message{
		Type: protocol.TypeExit, ExecID: "ex_t", ExitCode: &ec, Status: protocol.ExitCompleted,
	})
	last := cli.last()
	if last.Type != protocol.TypeExit || last.Truncated == nil || !*last.Truncated {
		t.Fatalf("want truncated exit got %+v", last)
	}
}

func TestClientCannotRevoke(t *testing.T) {
	h := NewHub(newMemStore())
	cli := &recSender{}
	h.Handle(cli, protocol.RoleClient, "dev_a", protocol.Message{Type: protocol.TypeRevoke, DeviceID: "dev_a"})
	if cli.last().Code != protocol.Unauthorized {
		t.Fatalf("got %+v", cli.last())
	}
}

func TestClientDropSendsCancelToDevice(t *testing.T) {
	h := NewHub(newMemStore())
	dev, cli := &recSender{}, &recSender{}
	h.RegisterDevice("dev_a", "secret", true, dev)
	h.mu.Lock()
	h.devices["dev_a"].inbound = true
	h.mu.Unlock()
	h.Handle(cli, protocol.RoleClient, "dev_a", protocol.Message{
		Type: protocol.TypeInvoke, ExecID: "ex_drop", Op: protocol.OpExec,
	})
	h.Drop(cli, protocol.RoleClient, "dev_a")
	found := false
	for _, m := range dev.got {
		if m.Type == protocol.TypeInvoke && m.Op == protocol.OpCancel && m.ExecID == "ex_drop" {
			found = true
		}
	}
	if !found {
		t.Fatalf("device did not get cancel on client drop: %+v", dev.got)
	}
	h.mu.Lock()
	_, still := h.inflight["ex_drop"]
	h.mu.Unlock()
	if still {
		t.Fatal("inflight must be deleted on client drop")
	}
}

func TestCancelUnknownExec(t *testing.T) {
	h := NewHub(newMemStore())
	cli := &recSender{}
	h.handleCancel(cli, "ex_missing")
	if cli.last().Code != protocol.UnknownExec {
		t.Fatalf("got %+v", cli.last())
	}
}

func TestInboundDisabled(t *testing.T) {
	h := NewHub(newMemStore())
	dev, cli := &recSender{}, &recSender{}
	h.RegisterDevice("dev_a", "secret", false, dev)
	h.Handle(cli, protocol.RoleClient, "dev_a", protocol.Message{
		Type: protocol.TypeInvoke, ExecID: "ex_1", Op: protocol.OpExec,
	})
	if !strings.Contains(cli.last().Code, protocol.InboundDisabled) && cli.last().Code != protocol.UnknownDevice && cli.last().Code != protocol.InboundDisabled {
		// inbound false → inbound_disabled if device online
	}
	if cli.last().Code != protocol.InboundDisabled {
		t.Fatalf("want inbound_disabled got %+v", cli.last())
	}
}
