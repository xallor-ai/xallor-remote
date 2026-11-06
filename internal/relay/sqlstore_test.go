package relay

import (
	"context"
	"errors"
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
	want := DeviceRecord{SecretHash: "sec", GrantHash: "gr", Inbound: true}
	if err := s.UpdateDevice(context.Background(), "dev_a", want); err != nil {
		t.Fatal(err)
	}
	got, err := s.LoadDevice(context.Background(), "dev_a")
	if err != nil || got != want {
		t.Fatalf("load %+v %v", got, err)
	}
	if err := s.DeleteDevice(context.Background(), "dev_a", "sec"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.LoadDevice(context.Background(), "dev_a"); !errors.Is(err, ErrDeviceNotFound) {
		t.Fatalf("deleted row: %v", err)
	}
}
