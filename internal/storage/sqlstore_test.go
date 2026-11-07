package storage

import (
	"context"
	"errors"
	"github.com/xallor-ai/xallor-remote/internal/access"
	"testing"
)

func TestSQLStoreDeviceRoundTrip(t *testing.T) {
	dir := t.TempDir()
	s, err := OpenSQLStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	if _, err := s.RegisterDevice(context.Background(), "dev_a", "sec", true); err != nil {
		t.Fatal(err)
	}
	want := setTestAccess(t, s, "dev_a", "gr", true)
	got, err := s.LoadDevice(context.Background(), "dev_a")
	if err != nil || got != want {
		t.Fatalf("load %+v %v", got, err)
	}
	if err := s.DeleteDevice(context.Background(), "dev_a", want.UID, "sec"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.LoadDevice(context.Background(), "dev_a"); !errors.Is(err, access.ErrDeviceNotFound) {
		t.Fatalf("deleted row: %v", err)
	}
}
