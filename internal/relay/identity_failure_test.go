package relay

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/xallor-ai/xallor-remote/internal/access"
	"github.com/xallor-ai/xallor-remote/internal/protocol"
)

func openIdentityTestStore(t *testing.T) *SQLStore {
	t.Helper()
	s, err := OpenSQLStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

// A storage outage must reject registration and client authentication, not act as an absent identity.
func TestIdentity_should_rejectRegistrationAndClient_whenStoreFails(t *testing.T) {
	s := openIdentityTestStore(t)
	h := NewHub(s, nil, nil)
	dev := &recSender{}
	if code := h.RegisterDevice("existing", "original", true, dev); code != "" {
		t.Fatal(code)
	}
	h.Handle(dev, protocol.RoleDevice, "existing", protocol.Message{Type: protocol.TypeDeviceSettings, RequestID: "settings", Inbound: protocol.Bool(true), GrantHash: protocol.SHA256Hex("grant")})
	if _, err := s.db.Exec(`DROP TABLE devices`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.LoadDevice(context.Background(), "existing"); err == nil || errors.Is(err, ErrDeviceNotFound) {
		t.Fatalf("storage failure masked: %v", err)
	}
	if code := h.RegisterDevice("existing", "other", true, &recSender{}); code != protocol.RelayError {
		t.Fatalf("registration: %s", code)
	}
	if code := h.RegisterClient("existing", "grant", "", &recSender{}); code != protocol.RelayError {
		t.Fatalf("cached authorization bypassed outage: %s", code)
	}
	if h.devices["existing"].conn != dev {
		t.Fatal("failed registration replaced the existing connection")
	}
}

// Each failed authorization mutation must leave durable and live state unchanged.
func TestIdentity_should_preserveAuthorization_whenMutationFails(t *testing.T) {
	for _, kind := range []string{protocol.TypeDeviceSettings, protocol.TypeRevoke} {
		t.Run(kind, func(t *testing.T) {
			s := openIdentityTestStore(t)
			h := NewHub(s, nil, nil)
			dev := &recSender{}
			if code := h.RegisterDevice("device", "secret", true, dev); code != "" {
				t.Fatal(code)
			}
			h.Handle(dev, protocol.RoleDevice, "device", protocol.Message{Type: protocol.TypeDeviceSettings, RequestID: "settings", Inbound: protocol.Bool(true), GrantHash: protocol.SHA256Hex("original")})
			before, err := s.LoadDevice(context.Background(), "device")
			if err != nil {
				t.Fatal(err)
			}
			statement := `CREATE TRIGGER reject_write BEFORE UPDATE ON devices BEGIN SELECT RAISE(ABORT, 'fixture write failure'); END;`
			if kind == protocol.TypeRevoke {
				statement = `CREATE TRIGGER reject_write BEFORE DELETE ON devices BEGIN SELECT RAISE(ABORT, 'fixture delete failure'); END;`
			}
			if _, err := s.db.Exec(statement); err != nil {
				t.Fatal(err)
			}
			h.Handle(dev, protocol.RoleDevice, "device", protocol.Message{Type: kind, RequestID: "settings", GrantHash: protocol.SHA256Hex("replacement"), Inbound: protocol.Bool(false)})
			if dev.last().Code != protocol.RelayError {
				t.Fatalf("mutation failure not reported: %+v", dev.last())
			}
			if h.DeviceOnline("device") {
				t.Fatal("failed authorization remained routable during close")
			}
			after, err := s.LoadDevice(context.Background(), "device")
			if err != nil || before != after || h.access["device"] != before.Binding() || !h.devices["device"].record.Inbound {
				t.Fatalf("failed mutation changed identity: %+v %v", after, err)
			}
		})
	}
}

// Two independent repositories racing to claim an ID must preserve exactly one secret.
func TestIdentity_should_preserveFirstClaim_whenRegistrationsRace(t *testing.T) {
	dir := t.TempDir()
	a, err := OpenSQLStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	b, err := OpenSQLStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	start := make(chan struct{})
	results := make(chan error, 2)
	var wg sync.WaitGroup
	for i, store := range []*SQLStore{a, b} {
		wg.Add(1)
		go func(s *SQLStore, secret string) {
			defer wg.Done()
			<-start
			_, err := s.RegisterDevice(context.Background(), "same-device", secret, true)
			results <- err
		}(store, []string{"one", "two"}[i])
	}
	close(start)
	wg.Wait()
	close(results)
	success, rejected := 0, 0
	for err := range results {
		if err == nil {
			success++
		} else if errors.Is(err, ErrDeviceIdentity) {
			rejected++
		} else {
			t.Fatal(err)
		}
	}
	if success != 1 || rejected != 1 {
		t.Fatalf("success=%d rejected=%d", success, rejected)
	}
	r, err := a.LoadDevice(context.Background(), "same-device")
	if err != nil {
		t.Fatal(err)
	}
	other := "one"
	if r.SecretHash == other {
		other = "two"
	}
	wrong := r
	wrong.SecretHash = other
	if _, err := b.UpdateDevice(context.Background(), "same-device", wrong); !errors.Is(err, ErrDeviceIdentity) {
		t.Fatal(err)
	}
	if err := b.DeleteDevice(context.Background(), "same-device", r.UID, other); !errors.Is(err, ErrDeviceIdentity) {
		t.Fatal(err)
	}
	if got, err := a.LoadDevice(context.Background(), "same-device"); err != nil || got != r {
		t.Fatalf("identity overwritten: %+v %v", got, err)
	}
}

// Closing a repository during shutdown must return errors instead of dereferencing a cleared DB pointer.
func TestIdentity_should_returnErrors_whenStoreClosed(t *testing.T) {
	s := openIdentityTestStore(t)
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := s.LoadDevice(context.Background(), "device"); err == nil || errors.Is(err, ErrDeviceNotFound) {
		t.Fatal(err)
	}
	if _, err := s.RegisterDevice(context.Background(), "device", "secret", true); err == nil {
		t.Fatal("register succeeded")
	}
	if _, err := s.UpdateDevice(context.Background(), "device", access.Device{}); err == nil {
		t.Fatal("update succeeded")
	}
	if err := s.DeleteDevice(context.Background(), "device", "uid", "secret"); err == nil {
		t.Fatal("delete succeeded")
	}
}
