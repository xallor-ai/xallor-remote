package runtimeowner

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func r4GuardFixture(t *testing.T) (Journal, ReconcileRequest, []byte) {
	t.Helper()
	j, tx := r4Fixture(t)
	if err := tx.Close(); err != nil {
		t.Fatal(err)
	}
	ns, err := j.NamespaceID()
	if err != nil {
		t.Fatal(err)
	}
	db, err := j.open(false)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var raw []byte
	var revision int64
	id := r4Request("1").ID
	if err := db.QueryRow("SELECT record,revision FROM operations WHERE id=?", id).Scan(&raw, &revision); err != nil {
		t.Fatal(err)
	}
	req := ReconcileRequest{OperationID: id, NamespaceSHA256: ns, ExpectedRevision: revision, ExpectedDigest: digestRecord(raw)}
	got, err := j.ReconcileR4(req, ReconciliationEvidence{Resources: []ResourceEvidence{{Kind: "jobs", State: "absent"}, {Kind: "runtime", State: "released"}, {Kind: "observers", State: "released"}}})
	if err != nil || got.Resolution == nil {
		t.Fatalf("resolve fixture: %+v, %v", got, err)
	}
	return j, req, raw
}

func r4GuardRow(t *testing.T, j Journal, id string) ([]byte, []byte, int64) {
	t.Helper()
	db, err := j.open(false)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var raw, resolution []byte
	var revision int64
	if err := db.QueryRow("SELECT record,resolution,revision FROM operations WHERE id=?", id).Scan(&raw, &resolution, &revision); err != nil {
		t.Fatal(err)
	}
	return raw, resolution, revision
}

// A resolved operation accepts only its original CAS; retries cannot substitute
// an older revision, the post-resolution revision, or another record digest.
func TestReconcileR4Guards_shouldRejectChangedCAS_whenAlreadyResolved(t *testing.T) {
	for _, kind := range []string{"older", "resolved-revision", "digest"} {
		t.Run(kind, func(t *testing.T) {
			j, req, original := r4GuardFixture(t)
			_, resolution, revision := r4GuardRow(t, j, req.OperationID)
			retry, err := j.ReconcileR4(req, ReconciliationEvidence{})
			if err != nil || retry.Resolution == nil || retry.Revision != revision {
				t.Fatalf("exact original CAS rejected: %+v %v", retry, err)
			}
			switch kind {
			case "older":
				req.ExpectedRevision--
			case "resolved-revision":
				req.ExpectedRevision = revision
			case "digest":
				req.ExpectedDigest = strings.Repeat("f", 64)
			}
			got, err := j.ReconcileR4(req, ReconciliationEvidence{})
			if !errors.Is(err, ErrRevisionConflict) || got.Resolution != nil {
				t.Errorf("changed %s CAS accepted: %+v %v", kind, got, err)
			}
			after, afterResolution, afterRevision := r4GuardRow(t, j, req.OperationID)
			if !bytes.Equal(original, after) || !bytes.Equal(resolution, afterResolution) || revision != afterRevision {
				t.Fatal("retry changed persisted row")
			}
		})
	}
}

// Unknown resolution fields must fail closed, preserving both original bytes
// and the malformed stored value so reconciliation does not repair history.
func TestReconcileR4Guards_shouldRejectUnknownFields_whenResolutionMalformed(t *testing.T) {
	j, req, original := r4GuardFixture(t)
	_, resolution, revision := r4GuardRow(t, j, req.OperationID)
	malformed := append([]byte(`{"unexpected":true,`), resolution[1:]...)
	db, err := j.openExistingWritable()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("UPDATE operations SET resolution=? WHERE id=?", malformed, req.OperationID); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	got, err := j.ReconcileR4(req, ReconciliationEvidence{})
	if !errors.Is(err, ErrInvalid) || got.Resolution != nil {
		t.Errorf("unknown resolution field accepted: %+v %v", got, err)
	}
	after, afterResolution, afterRevision := r4GuardRow(t, j, req.OperationID)
	if !bytes.Equal(original, after) || !bytes.Equal(malformed, afterResolution) || revision != afterRevision {
		t.Fatal("malformed row was changed")
	}
}

// Observation of a missing profile or database must leave it absent.
func TestReconcileR4Guards_shouldLeaveMissingProfileAndDatabaseAbsent(t *testing.T) {
	j := Journal{Dir: filepath.Join(t.TempDir(), "missing")}
	ns, err := j.NamespaceID()
	if err != nil {
		t.Fatal(err)
	}
	req := ReconcileRequest{OperationID: strings.Repeat("1", 32), NamespaceSHA256: ns, ExpectedRevision: 1}
	if _, err := j.ReconcileR4(req, ReconciliationEvidence{}); !errors.Is(err, ErrUnknown) {
		t.Fatalf("missing profile: %v", err)
	}
	if _, err := os.Stat(j.Dir); !os.IsNotExist(err) {
		t.Fatalf("profile created: %v", err)
	}
	if err := os.Mkdir(j.Dir, 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := j.ReconcileR4(req, ReconciliationEvidence{}); !errors.Is(err, ErrUnknown) {
		t.Fatalf("missing database: %v", err)
	}
	entries, err := os.ReadDir(j.Dir)
	if err != nil || len(entries) != 0 {
		t.Fatalf("missing database observation wrote files: %v %v", entries, err)
	}
}
