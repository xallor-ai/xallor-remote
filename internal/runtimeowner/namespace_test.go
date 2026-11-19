package runtimeowner

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func completedNamespaceFixture(t *testing.T, j Journal, id string) {
	t.Helper()
	tx, err := j.Admit(request(id))
	if err != nil {
		t.Fatal(err)
	}
	bindFixture(t, tx, boundIntent("prepare", strings.Repeat("b", 64)).Before, strings.Repeat("b", 64))
	prefixFixture(t, tx, "commit")
	defer tx.Close()
	for _, name := range []string{"commit", "ready"} {
		if err := testEffect(tx, name, strings.Repeat("b", 64), func() (EffectState, error) { return EffectConfirmed, nil }); err != nil {
			t.Fatal(err)
		}
	}
	if err := tx.Finish(Succeeded); err != nil {
		t.Fatal(err)
	}
}

// A byte-for-byte copy is still another profile's history. It cannot certify
// completion, appear idle, or accept a new mutation in the destination.
func TestJournal_should_refuseCopiedNamespace_beforeObservationOrAdmission(t *testing.T) {
	source, destination := journal(t), journal(t)
	completedNamespaceFixture(t, source, "1")
	raw, err := os.ReadFile(filepath.Join(source.Dir, "operations.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(destination.Dir, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(destination.Dir, "operations.db")
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := destination.Query(request("1").ID); !errors.Is(err, ErrInvalid) {
		t.Error("copied completion was accepted", err)
	}
	if _, err := destination.Active(); !errors.Is(err, ErrInvalid) {
		t.Error("foreign journal was treated as local idle state", err)
	}
	if tx, err := destination.Admit(request("2")); !errors.Is(err, ErrInvalid) {
		if tx != nil {
			_ = tx.Close()
		}
		t.Error("foreign namespace admitted a new transaction", err)
	}
	if after, err := os.ReadFile(path); err != nil || !bytes.Equal(after, raw) {
		t.Error("foreign journal was rewritten", err)
	}
	if r, err := source.Query(request("1").ID); err != nil || r.Outcome != Succeeded {
		t.Fatal("original namespace lost its history", r, err)
	}
}

// Copying just a record into a valid destination database must not evade the
// database-level binding. Other valid local records remain queryable.
func TestJournal_should_refuseForeignRecordInsideLocalNamespace(t *testing.T) {
	source, destination := journal(t), journal(t)
	completedNamespaceFixture(t, source, "1")
	completedNamespaceFixture(t, destination, "2")
	src, err := source.open(false)
	if err != nil {
		t.Fatal(err)
	}
	var raw []byte
	err = src.QueryRow("SELECT record FROM operations WHERE id=?", request("1").ID).Scan(&raw)
	_ = src.Close()
	if err != nil {
		t.Fatal(err)
	}
	dst, err := destination.open(true)
	if err != nil {
		t.Fatal(err)
	}
	_, err = dst.Exec("INSERT INTO operations(id,record) VALUES(?,?)", request("1").ID, raw)
	_ = dst.Close()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := destination.Query(request("1").ID); !errors.Is(err, ErrInvalid) {
		t.Fatal("foreign record was accepted as a local result", err)
	}
	if r, err := destination.Query(request("2").ID); err != nil || r.Outcome != Succeeded {
		t.Fatal("valid local record was lost", r, err)
	}
}

// Unpublished schema 1 has no namespace evidence. Observation or admission
// must not stamp it with the current profile and manufacture that evidence.
func TestJournal_should_refuseLegacySchema_withoutAutomaticRebinding(t *testing.T) {
	for _, version := range []int{1, 2, 3, 4, 5, 6} {
		t.Run(fmt.Sprint(version), func(t *testing.T) {
			j := journal(t)
			completedNamespaceFixture(t, j, "1")
			db, err := j.open(true)
			if err != nil {
				t.Fatal(err)
			}
			_, err = db.Exec(fmt.Sprintf("PRAGMA user_version=%d", version))
			_ = db.Close()
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(j.Dir, "operations.db")
			before, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := j.Query(request("1").ID); !errors.Is(err, ErrInvalid) {
				t.Error("unbound legacy history was accepted", err)
			}
			if tx, err := j.Admit(request("2")); !errors.Is(err, ErrInvalid) {
				if tx != nil {
					_ = tx.Close()
				}
				t.Error("legacy history was rebound during admission", err)
			}
			if after, err := os.ReadFile(path); err != nil || !bytes.Equal(after, before) {
				t.Error("legacy journal was migrated implicitly", err)
			}
		})
	}
}

// Every journal transition retains the namespace captured at admission, even
// if an internal caller constructs a structurally valid record for another one.
func TestTransaction_should_refuseNamespaceChange_onSave(t *testing.T) {
	j := journal(t)
	tx, err := j.Admit(request("1"))
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Close()
	changed := tx.record
	changed.NamespaceSHA256 = strings.Repeat("f", 64)
	if err := tx.save(changed); !errors.Is(err, ErrInvalid) {
		t.Fatal("transition rewrote the admitted namespace", err)
	}
}

// Namespace correlation is independent of working-directory changes and never
// initializes a missing profile merely to calculate its identity.
func TestNamespace_should_requireAbsoluteLocation_withoutCreatingIt(t *testing.T) {
	for _, dir := range []string{"", "relative/lifecycle"} {
		if _, err := (Journal{Dir: dir}).NamespaceID(); !errors.Is(err, ErrInvalid) {
			t.Fatal("relative namespace accepted", dir, err)
		}
	}
	j := journal(t)
	a, err := j.NamespaceID()
	if err != nil {
		t.Fatal(err)
	}
	b, err := (Journal{Dir: j.Dir + string(os.PathSeparator) + "."}).NamespaceID()
	if err != nil || a != b {
		t.Fatal("clean namespace changed", a, b, err)
	}
	if _, err := os.Stat(j.Dir); !os.IsNotExist(err) {
		t.Fatal("namespace observation created a directory", err)
	}
}

// Missing or corrupt identity metadata must not look like an absent journal
// eligible for bootstrap, and opening must never repair the damaged database.
func TestJournal_should_refuseDamagedNamespaceMetadata_withoutRepair(t *testing.T) {
	for _, change := range []string{"DELETE FROM journal_identity", "DROP TABLE journal_identity",
		"UPDATE journal_identity SET namespace_sha256='" + strings.Repeat("z", 64) + "'"} {
		t.Run(change, func(t *testing.T) {
			j := journal(t)
			completedNamespaceFixture(t, j, "1")
			db, err := j.open(true)
			if err != nil {
				t.Fatal(err)
			}
			_, err = db.Exec(change)
			_ = db.Close()
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(j.Dir, "operations.db")
			before, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if err := j.RefuseBootstrap(); !errors.Is(err, ErrInvalid) {
				t.Fatal("damaged binding admitted bootstrap", err)
			}
			if after, err := os.ReadFile(path); err != nil || !bytes.Equal(before, after) {
				t.Fatal("binding repaired implicitly", err)
			}
		})
	}
}
