package identity

import (
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func newDeviceStore(t *testing.T) *Store {
	t.Helper()
	s, err := LoadFrom(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func deviceDatabase(t *testing.T, dir string) *sql.DB {
	t.Helper()
	db, err := openDeviceDB(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func mustExecDevice(t *testing.T, db *sql.DB, query string, args ...any) {
	t.Helper()
	if _, err := db.Exec(query, args...); err != nil {
		t.Fatal(err)
	}
}

// Real SQLite writer and reader locks prevent statement execution or commit.
// Neither cache nor durable state may advance; a later explicit request can work.
func TestDevice_should_preserveState_whenTransactionCannotCommit(t *testing.T) {
	for _, lock := range []string{"writer", "reader"} {
		for _, op := range []string{"issue", "enable", "disable", "clear", "wipe"} {
			t.Run(lock+"/"+op, func(t *testing.T) {
				s := newDeviceStore(t)
				if op != "enable" {
					if _, err := s.IssueGrant(); err != nil {
						t.Fatal(err)
					}
				}
				before := s.device
				db := deviceDatabase(t, s.Dir)
				if lock == "writer" {
					mustExecDevice(t, db, "BEGIN IMMEDIATE")
				} else {
					mustExecDevice(t, db, "BEGIN")
					if _, err := readDeviceRecord(db.QueryRow(selectDeviceRecord)); err != nil {
						t.Fatal(err)
					}
				}
				var err error
				switch op {
				case "issue":
					_, err = s.IssueGrant()
				case "enable":
					_, err = s.SetInbound(true)
				case "disable":
					_, err = s.SetInbound(false)
				case "clear":
					err = s.ClearGrant()
				case "wipe":
					err = s.Wipe()
				}
				if err == nil {
					t.Fatal("blocked transaction succeeded")
				}
				if s.device != before {
					t.Fatal("failed mutation changed in-memory identity")
				}
				mustExecDevice(t, db, "ROLLBACK")
				after, err := loadDeviceRecord(s.Dir, false)
				if err != nil || after != before {
					t.Fatal("failed mutation changed durable identity", err)
				}
				if _, err := s.IssueGrant(); err != nil {
					t.Fatal("explicit request after unlock failed", err)
				}
			})
		}
	}
}

// Two independently loaded Stores represent stale writers. Revision CAS must
// reject an overwrite; refresh may accept only the same immutable identity.
func TestDevice_should_rejectStaleWrite_and_refreshSameIdentity(t *testing.T) {
	a := newDeviceStore(t)
	b, err := LoadFrom(a.Dir)
	if err != nil {
		t.Fatal(err)
	}
	grant, err := a.IssueGrant()
	if err != nil {
		t.Fatal(err)
	}
	if _, err = b.SetInbound(false); !errors.Is(err, errIdentityChanged) {
		t.Fatal("stale write accepted", err)
	}
	if err = b.RefreshDevice(); err != nil {
		t.Fatal(err)
	}
	if b.device.Grant != grant || !b.device.Inbound {
		t.Fatal("refresh lost committed grant")
	}
	if _, err = b.SetInbound(false); err != nil {
		t.Fatal(err)
	}
	if err = a.RefreshDevice(); err != nil {
		t.Fatal(err)
	}
	if a.device.Grant != grant || a.device.Inbound {
		t.Fatal("refresh lost disabled state")
	}
}

// Reset is a durable tombstone. Stale handles cannot resurrect it or adopt the
// next startup's identity, including after another process initializes it.
func TestDevice_should_rejectOldHandle_whenIdentityReset(t *testing.T) {
	a := newDeviceStore(t)
	b, err := LoadFrom(a.Dir)
	if err != nil {
		t.Fatal(err)
	}
	old := a.device
	if err = a.Wipe(); err != nil {
		t.Fatal(err)
	}
	if err = b.RefreshDevice(); !errors.Is(err, errIdentityChanged) {
		t.Fatal("reset refresh accepted", err)
	}
	if _, err = b.IssueGrant(); !errors.Is(err, errIdentityChanged) {
		t.Fatal("reset resurrected", err)
	}
	fresh, err := LoadFrom(a.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if fresh.device.DeviceID == old.DeviceID || fresh.device.Secret == old.Secret || fresh.device.Grant != "" || fresh.device.Inbound {
		t.Fatal("reset did not initialize a clean identity")
	}
	if err = b.RefreshDevice(); !errors.Is(err, errIdentityChanged) {
		t.Fatal("old Runtime adopted new identity", err)
	}
	if _, err = a.IssueGrant(); !errors.Is(err, errIdentityChanged) {
		t.Fatal("reset Store adopted new identity", err)
	}
}

// Missing or invalid canonical records must not silently generate credentials.
func TestDevice_should_failClosed_whenCanonicalRecordCorrupted(t *testing.T) {
	for _, query := range []string{"DELETE FROM identity", "UPDATE identity SET device_id='broken'", "UPDATE identity SET secret=''", "UPDATE identity SET active=0", "PRAGMA user_version=99"} {
		t.Run(query, func(t *testing.T) {
			s := newDeviceStore(t)
			db := deviceDatabase(t, s.Dir)
			mustExecDevice(t, db, query)
			if _, err := LoadFrom(s.Dir); err == nil {
				t.Fatal("corrupt canonical state was accepted")
			}
			if err := s.RefreshDevice(); err == nil {
				t.Fatal("live refresh accepted corrupt canonical state")
			}
			if query == "PRAGMA user_version=99" {
				if _, err := s.IssueGrant(); err == nil {
					t.Fatal("old writer accepted unsupported format")
				}
			}
		})
	}
}

// Deleting the database while a Store is alive must fail writes and refreshes,
// without recreating a fresh database behind the live Runtime.
func TestDevice_should_notCreateDatabase_whenLiveStateIsMissing(t *testing.T) {
	s := newDeviceStore(t)
	path := filepath.Join(s.Dir, deviceDBName)
	if err := os.Rename(path, path+".saved"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.IssueGrant(); err == nil {
		t.Fatal("write accepted missing database")
	}
	if err := s.RefreshDevice(); err == nil {
		t.Fatal("refresh accepted missing database")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("missing database was recreated")
	}
}

// Concurrent local enable requests share one grant and commit complete states.
// This exercises the old check/unlock/IssueGrant race under the race detector.
func TestDevice_should_shareGrant_whenConcurrentEnable(t *testing.T) {
	s := newDeviceStore(t)
	var wg sync.WaitGroup
	grants := make(chan string, 12)
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			g, err := s.SetInbound(true)
			if err != nil {
				t.Error(err)
			}
			grants <- g
		}()
	}
	wg.Wait()
	close(grants)
	var first string
	for g := range grants {
		if first == "" {
			first = g
		}
		if g == "" || g != first {
			t.Fatal("concurrent enable rotated grant")
		}
	}
	loaded, err := LoadFrom(s.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.device.Grant != first || !loaded.device.Inbound {
		t.Fatal("durable enable is incomplete")
	}
}
