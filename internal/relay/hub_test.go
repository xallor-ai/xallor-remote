package relay

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/xallor-ai/xallor-remote/internal/access"
	"github.com/xallor-ai/xallor-remote/internal/admission"
	"github.com/xallor-ai/xallor-remote/internal/protocol"
)

type memStore struct {
	mu   sync.Mutex
	rows map[string]access.Device
}

func newMemStore() *memStore {
	return &memStore{rows: map[string]access.Device{}}
}

func (m *memStore) Authority() string { return strings.Repeat("a", 32) }

func (m *memStore) LoadDevice(ctx context.Context, id string) (access.Device, error) {
	if err := ctx.Err(); err != nil {
		return access.Device{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.rows[id]
	if !ok {
		return access.Device{}, access.ErrDeviceNotFound
	}
	return r, nil
}

func (m *memStore) RegisterDevice(ctx context.Context, id, secretHash string, inbound bool) (access.Device, error) {
	if err := ctx.Err(); err != nil {
		return access.Device{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if r, ok := m.rows[id]; ok {
		if r.SecretHash != secretHash {
			return access.Device{}, access.ErrDeviceIdentity
		}
		return r, nil
	}
	r, err := access.NewDevice(secretHash, inbound)
	if err != nil {
		return access.Device{}, err
	}
	m.rows[id] = r
	return r, nil
}

func (m *memStore) UpdateDevice(ctx context.Context, id string, r access.Device) (access.Device, error) {
	if err := ctx.Err(); err != nil {
		return access.Device{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	old, ok := m.rows[id]
	if !ok || old.SecretHash != r.SecretHash || old.UID != r.UID {
		return access.Device{}, access.ErrDeviceIdentity
	}
	r, err := old.WithAccess(r.GrantHash, r.Inbound)
	if err != nil {
		return access.Device{}, err
	}
	m.rows[id] = r
	return r, nil
}

func (m *memStore) DeleteDevice(ctx context.Context, id, uid, secretHash string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if old, ok := m.rows[id]; !ok || old.SecretHash != secretHash || old.UID != uid {
		return access.ErrDeviceIdentity
	}
	delete(m.rows, id)
	return nil
}

type recSender struct {
	mu  sync.Mutex
	got []protocol.Message
}

func (r *recSender) Enqueue(m protocol.Message) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.got = append(r.got, m)
	return nil
}

func (r *recSender) Close() {}

func (r *recSender) Context() context.Context { return context.Background() }

func (r *recSender) last() protocol.Message {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.got) == 0 {
		return protocol.Message{}
	}
	return r.got[len(r.got)-1]
}

func TestInvokeRoutesAndExitDeletesInflight(t *testing.T) {
	h := NewHub(newMemStore(), nil, nil, nil, admission.New(admission.Limits{PerDevice: protocol.MaxDeviceRequests}))
	dev, cli := &recSender{}, &recSender{}
	if code := h.RegisterDevice("dev_a", "secret", true, dev); code != "" {
		t.Fatal(code)
	}
	grant := "xr_grant_test"
	h.Handle(dev, protocol.RoleDevice, "dev_a", protocol.Message{Type: protocol.TypeDeviceSettings, RequestID: "settings", Inbound: protocol.Bool(true), GrantHash: protocol.SHA256Hex(grant)})
	if code := h.RegisterClient("dev_a", grant, "", cli); code != "" {
		t.Fatal(code)
	}

	h.Handle(cli, protocol.RoleClient, "dev_a", protocol.Message{
		Type: protocol.TypeInvoke, ExecID: "ex_1", Op: protocol.OpExec,
	})
	routeID := dev.last().ExecID
	if routeID == "" || routeID == "ex_1" {
		t.Fatalf("device did not get invoke: %+v", dev.last())
	}

	h.Handle(dev, protocol.RoleDevice, "dev_a", protocol.Message{
		Type: protocol.TypeStdout, ExecID: routeID, Data: "hi\n",
	})
	if cli.last().Data != "hi\n" || cli.last().ExecID != "ex_1" {
		t.Fatalf("client stdout %+v", cli.last())
	}

	ec := 0
	h.Handle(dev, protocol.RoleDevice, "dev_a", protocol.Message{
		Type: protocol.TypeExit, ExecID: routeID, ExitCode: &ec, Status: protocol.ExitCompleted,
	})
	if cli.last().Type != protocol.TypeExit {
		t.Fatalf("want exit got %+v", cli.last())
	}

	h.Handle(dev, protocol.RoleDevice, "dev_a", protocol.Message{
		Type: protocol.TypeStdout, ExecID: routeID, Data: "late\n",
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
	h := NewHub(newMemStore(), nil, nil, nil, admission.New(admission.Limits{PerDevice: protocol.MaxDeviceRequests}))
	dev, cli := &recSender{}, &recSender{}
	h.RegisterDevice("dev_a", "secret", true, dev)
	authorizeTestClient(t, h, dev, cli)
	h.mu.Lock()
	h.devices["dev_a"].record.Inbound = true
	h.mu.Unlock()
	h.Handle(cli, protocol.RoleClient, "dev_a", protocol.Message{
		Type: protocol.TypeInvoke, ExecID: "ex_t", Op: protocol.OpExec,
	})
	routeID := dev.last().ExecID
	big := strings.Repeat("x", protocol.MaxFrameBytes+1)
	h.Handle(dev, protocol.RoleDevice, "dev_a", protocol.Message{
		Type: protocol.TypeStdout, ExecID: routeID, Data: big,
	})
	ec := 0
	h.Handle(dev, protocol.RoleDevice, "dev_a", protocol.Message{
		Type: protocol.TypeExit, ExecID: routeID, ExitCode: &ec, Status: protocol.ExitCompleted,
	})
	last := cli.last()
	if last.Type != protocol.TypeExit || last.Truncated == nil || !*last.Truncated {
		t.Fatalf("want truncated exit got %+v", last)
	}
}

func TestClientCannotRevoke(t *testing.T) {
	h := NewHub(newMemStore(), nil, nil, nil, admission.New(admission.Limits{PerDevice: protocol.MaxDeviceRequests}))
	cli := &recSender{}
	h.Handle(cli, protocol.RoleClient, "dev_a", protocol.Message{Type: protocol.TypeRevoke, DeviceID: "dev_a"})
	if cli.last().Code != protocol.Unauthorized {
		t.Fatalf("got %+v", cli.last())
	}
}

func TestClientDropSendsCancelToDevice(t *testing.T) {
	h := NewHub(newMemStore(), nil, nil, nil, admission.New(admission.Limits{PerDevice: protocol.MaxDeviceRequests}))
	dev, cli := &recSender{}, &recSender{}
	h.RegisterDevice("dev_a", "secret", true, dev)
	authorizeTestClient(t, h, dev, cli)
	h.mu.Lock()
	h.devices["dev_a"].record.Inbound = true
	h.mu.Unlock()
	h.Handle(cli, protocol.RoleClient, "dev_a", protocol.Message{
		Type: protocol.TypeInvoke, ExecID: "ex_drop", Op: protocol.OpExec,
	})
	routeID := dev.last().ExecID
	h.Drop(cli, protocol.RoleClient, "dev_a")
	found := false
	for _, m := range dev.got {
		if m.Type == protocol.TypeInvoke && m.Op == protocol.OpCancel && m.ExecID == routeID {
			found = true
		}
	}
	if !found {
		t.Fatalf("device did not get cancel on client drop: %+v", dev.got)
	}
	h.mu.Lock()
	_, still := h.inflight[routeID]
	h.mu.Unlock()
	if still {
		t.Fatal("inflight must be deleted on client drop")
	}
}

func TestCancelUnknownExec(t *testing.T) {
	h := NewHub(newMemStore(), nil, nil, nil, admission.New(admission.Limits{PerDevice: protocol.MaxDeviceRequests}))
	cli := &recSender{}
	h.handleCancel(cli, "ex_missing")
	if cli.last().Code != protocol.UnknownExec {
		t.Fatalf("got %+v", cli.last())
	}
}

func TestInboundDisabled(t *testing.T) {
	h := NewHub(newMemStore(), nil, nil, nil, admission.New(admission.Limits{PerDevice: protocol.MaxDeviceRequests}))
	dev, cli := &recSender{}, &recSender{}
	h.RegisterDevice("dev_a", "secret", false, dev)
	authorizeTestClient(t, h, dev, cli)
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
