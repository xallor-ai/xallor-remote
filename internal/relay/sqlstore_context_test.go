package relay

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// A real external SQLite writer lock must not turn a cancelled registration into a committed identity.
func TestSQLIdentity_should_notCommit_whenCancelledBehindWriterLock(t *testing.T) {
	dir := t.TempDir()
	s, err := OpenSQLStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	other, err := sql.Open("sqlite", filepath.Join(dir, "relay.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	if _, err := other.Exec(`BEGIN IMMEDIATE`); err != nil {
		t.Fatal(err)
	}
	defer other.Exec(`ROLLBACK`)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	started := time.Now()
	_, err = s.RegisterDevice(ctx, "device", "secret", true)
	if time.Since(started) > 2*time.Second {
		t.Fatal("native busy wait masked cancellation")
	}
	if err == nil {
		t.Fatal("registered behind held write lock")
	}
	if ctx.Err() == nil {
		t.Fatalf("fixture did not reach cancellation: %v", err)
	}
	if _, err := other.Exec(`ROLLBACK`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.LoadDevice(context.Background(), "device"); !errors.Is(err, ErrDeviceNotFound) {
		t.Fatalf("cancelled registration committed: %v", err)
	}
}

// Busy retries after rollback must converge to one identity when an external writer releases its lock.
func TestSQLIdentity_should_registerOnce_whenWriterContentionClears(t *testing.T) {
	dir := t.TempDir()
	s, err := OpenSQLStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	other, err := sql.Open("sqlite", filepath.Join(dir, "relay.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	other.SetMaxOpenConns(1)
	if _, err := other.Exec(`BEGIN IMMEDIATE`); err != nil {
		t.Fatal(err)
	}
	finished := make(chan error, 1)
	go func() { time.Sleep(200 * time.Millisecond); _, err := other.Exec(`ROLLBACK`); finished <- err }()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_, registerErr := s.RegisterDevice(ctx, "device", "secret", true)
	if err := <-finished; err != nil {
		t.Fatal(err)
	}
	if registerErr != nil {
		t.Fatal(registerErr)
	}
	if _, err := s.RegisterDevice(ctx, "device", "other", true); !errors.Is(err, ErrDeviceIdentity) {
		t.Fatalf("identity replaced: %v", err)
	}
	var count int
	if err := s.db.QueryRow(`SELECT count(*) FROM devices`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("rows=%d error=%v", count, err)
	}
}

// URI configuration must preserve the exact UTF-8 directory, including reserved URL characters.
func TestSQLStore_should_preservePath_whenDirectoryContainsReservedCharacters(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "设备 # & % space")
	s, err := OpenSQLStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.RegisterDevice(context.Background(), "device", "secret", true); err != nil {
		s.Close()
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "relay.db")); err != nil {
		t.Fatal(err)
	}
	s, err = OpenSQLStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err := s.LoadDevice(context.Background(), "device"); err != nil {
		t.Fatal(err)
	}
}
