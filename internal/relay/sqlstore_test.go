package relay

import (
	"errors"
	"testing"
	"time"
)

func TestSQLStoreDeviceRoundTrip(t *testing.T) {
	dir := t.TempDir()
	s, err := OpenSQLStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	if _, err := s.RegisterDevice("dev_a", "sec", true); err != nil {
		t.Fatal(err)
	}
	want := DeviceRecord{SecretHash: "sec", GrantHash: "gr", Inbound: true}
	if err := s.UpdateDevice("dev_a", want); err != nil {
		t.Fatal(err)
	}
	got, err := s.LoadDevice("dev_a")
	if err != nil || got != want {
		t.Fatalf("load %+v %v", got, err)
	}
	if err := s.DeleteDevice("dev_a", "sec"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.LoadDevice("dev_a"); !errors.Is(err, ErrDeviceNotFound) {
		t.Fatalf("deleted row: %v", err)
	}
}

func TestSQLStoreAuditOmitsFullCommand(t *testing.T) {
	s, err := OpenSQLStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	s.AppendAudit(AuditEntry{
		DeviceID: "dev_a", Op: "exec", ExecID: "ex_1",
		Decision: "accept", ArgsPreview: "whoami", ArgsDigest: "abc",
	})
	time.Sleep(20 * time.Millisecond)
	var n int
	if err := s.db.QueryRow(`SELECT count(*) FROM audit WHERE args_preview=?`, "whoami").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("audit rows %d", n)
	}
}

func TestAuditPreviewWriteIsPathOnly(t *testing.T) {
	payload := []byte(`{"path":"a.txt","content":"SECRET-BODY-SHOULD-NOT-PREVIEW"}`)
	prev, dig := auditPreview("write", payload)
	if prev != "a.txt" {
		t.Fatalf("preview %q", prev)
	}
	if dig == "" || prev == "SECRET-BODY-SHOULD-NOT-PREVIEW" {
		t.Fatal("digest/preview leaked body")
	}
}
