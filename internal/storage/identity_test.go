package storage

import (
	"context"
	"errors"
	"github.com/xallor-ai/xallor-remote/internal/access"
	"sync"
	"testing"
	"time"
)

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
		} else if errors.Is(err, access.ErrDeviceIdentity) {
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
	if _, err := b.UpdateDevice(context.Background(), "same-device", wrong); !errors.Is(err, access.ErrDeviceIdentity) {
		t.Fatal(err)
	}
	if err := b.DeleteDevice(context.Background(), "same-device", r.UID, other); !errors.Is(err, access.ErrDeviceIdentity) {
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
	if _, err := s.LoadDevice(context.Background(), "device"); err == nil || errors.Is(err, access.ErrDeviceNotFound) {
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

// Every identity operation must respect context cancellation while waiting for a SQL connection.
func TestSQLIdentity_should_cancelPoolWait_withoutMutatingRows(t *testing.T) {
	s := openIdentityTestStore(t)
	for name, call := range map[string]func(context.Context) error{
		"register": func(ctx context.Context) error { _, err := s.RegisterDevice(ctx, "device", "secret", true); return err },
		"load":     func(ctx context.Context) error { _, err := s.LoadDevice(ctx, "device"); return err },
		"update":   func(ctx context.Context) error { _, err := s.UpdateDevice(ctx, "device", access.Device{}); return err },
		"delete":   func(ctx context.Context) error { return s.DeleteDevice(ctx, "device", "uid", "secret") },
	} {
		t.Run(name, func(t *testing.T) {
			held, err := s.db.Conn(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			defer held.Close()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			before := s.db.Stats().WaitCount
			result := make(chan error, 1)
			go func() { result <- call(ctx) }()
			waitCondition(t, func() bool { return s.db.Stats().WaitCount > before })
			cancel()
			select {
			case err := <-result:
				if !errors.Is(err, context.Canceled) {
					t.Fatal(err)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("SQL pool wait ignored cancellation")
			}
		})
	}
	if _, err := s.LoadDevice(context.Background(), "device"); !errors.Is(err, access.ErrDeviceNotFound) {
		t.Fatalf("cancelled mutation changed state: %v", err)
	}
}
