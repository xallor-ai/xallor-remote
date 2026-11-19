package runtimeowner

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xallor-ai/xallor-remote/internal/filelock"
	"github.com/xallor-ai/xallor-remote/internal/release"
)

// r1JournalFixture leaves an uncertain requested commit with no durable ready
// acknowledgement, which is the only original state R1 may resolve.
func r1JournalFixture(t *testing.T) (Journal, string, int64, []byte, Record) {
	t.Helper()
	j := journal(t)
	tx, err := j.Admit(request("1"))
	if err != nil {
		t.Fatal(err)
	}
	bindFixture(t, tx, releaseObserveSelection(), strings.Repeat("b", 64))
	prefixFixture(t, tx, "start")
	if err := testApply(tx, "start", strings.Repeat("b", 64), func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	if err := testApply(tx, "commit", strings.Repeat("b", 64), func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	if err := tx.Finish(Uncertain); err != nil {
		t.Fatal(err)
	}
	id := request("1").ID
	if err := tx.Close(); err != nil {
		t.Fatal(err)
	}
	db, err := j.openExistingWritable()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var raw []byte
	var revision int64
	if err := db.QueryRow("SELECT record,revision FROM operations WHERE id=?", id).Scan(&raw, &revision); err != nil {
		t.Fatal(err)
	}
	var record Record
	if err := json.Unmarshal(raw, &record); err != nil {
		t.Fatal(err)
	}
	return j, id, revision, raw, record
}

func releaseObserveSelection() release.SelectionEvidence {
	return release.ObserveSelection(release.State{})
}

// A complete R1 observation appends only a resolution and leaves the original
// record bytes, outcome, and effects unchanged.
func TestReconcileR1_shouldAppendRequestedAppliedResolution_withoutMutatingOriginal(t *testing.T) {
	j, id, revision, rawBefore, record := r1JournalFixture(t)
	ns, _ := j.NamespaceID()
	evidence := r1EvidenceFor(t, record)
	got, err := j.ReconcileR1(ReconcileRequest{OperationID: id, NamespaceSHA256: ns, ExpectedRevision: revision}, evidence)
	if err != nil || got.Resolution == nil || got.Resolution.RequestResult != RequestRequestedApplied || got.Resolution.EvidenceEvaluator != R1EvaluatorCapability() || got.Disposition != DispositionResolved {
		t.Fatalf("reconcile=%+v err=%v", got, err)
	}
	if got.Resolution.HistoricalReadyAckUncertain != true || got.ResourceState != ResourceSettled || got.EffectiveActive {
		t.Fatalf("invalid R1 resolution=%+v active=%v", got.Resolution, got.EffectiveActive)
	}
	db, err := j.open(false)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var rawAfter []byte
	if err := db.QueryRow("SELECT record FROM operations WHERE id=?", id).Scan(&rawAfter); err != nil {
		t.Fatal(err)
	}
	if string(rawBefore) != string(rawAfter) {
		t.Fatal("R1 resolution mutated immutable original record")
	}
	observed, err := j.Query(id)
	if err != nil || observed.Outcome != Uncertain || len(observed.Effects) != len(record.Effects) {
		t.Fatalf("resolved original is no longer queryable: %+v %v", observed, err)
	}
}

// R1 must retain an unresolved operation when present-state evidence is not
// sufficient to prove the requested candidate is active.
func TestReconcileR1_shouldRefuseReadyOnlyBaselineAndActiveDebt(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*R1Evidence)
	}{
		{"ready-only", func(e *R1Evidence) { e.Current.ReadyStable = false }},
		{"baseline", func(e *R1Evidence) { e.Current.Selection.Current.SHA256 = strings.Repeat("c", 64) }},
		{"active-worker", func(e *R1Evidence) { e.WorkerActive = true }},
		{"unknown-debt", func(e *R1Evidence) { e.Resources[0].State = "unknown" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			j, id, revision, _, record := r1JournalFixture(t)
			ns, _ := j.NamespaceID()
			e := r1EvidenceFor(t, record)
			tc.mutate(&e)
			got, err := j.ReconcileR1(ReconcileRequest{OperationID: id, NamespaceSHA256: ns, ExpectedRevision: revision}, e)
			if err != nil || got.Resolution != nil || got.Disposition != DispositionUnresolved {
				t.Fatalf("refusal=%+v err=%v", got, err)
			}
		})
	}
}

func TestReconcileR1_shouldRejectMalformedEvidenceWithoutWriting(t *testing.T) {
	j, id, revision, _, record := r1JournalFixture(t)
	ns, _ := j.NamespaceID()
	e := r1EvidenceFor(t, record)
	e.RecordSHA256 = strings.Repeat("e", 64)
	got, err := j.ReconcileR1(ReconcileRequest{OperationID: id, NamespaceSHA256: ns, ExpectedRevision: revision}, e)
	if err != nil || got.Resolution != nil || got.Disposition != DispositionUnresolved {
		t.Fatalf("malformed evidence was written: %+v %v", got, err)
	}
}

func TestReconcileR1_shouldRejectOversizedOriginalRow(t *testing.T) {
	j, id, revision, _, _ := r1JournalFixture(t)
	db, err := j.openExistingWritable()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("PRAGMA ignore_check_constraints=ON"); err != nil {
		db.Close()
		t.Skipf("sqlite does not support bypassing fixture checks: %v", err)
	}
	if _, err := db.Exec("UPDATE operations SET record=? WHERE id=?", `{"outcome":"uncertain","padding":"`+strings.Repeat("x", maxRecordBytes)+`"}`, id); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	ns, _ := j.NamespaceID()
	got, err := j.ReconcileR1(ReconcileRequest{OperationID: id, NamespaceSHA256: ns, ExpectedRevision: revision}, R1Evidence{})
	if !errors.Is(err, ErrInvalid) || got.Resolution != nil {
		t.Fatalf("oversized row accepted: %+v %v", got, err)
	}
}

func TestReconcileR1_shouldRejectStaleCASAndNamespace(t *testing.T) {
	j, id, revision, _, record := r1JournalFixture(t)
	ns, _ := j.NamespaceID()
	e := r1EvidenceFor(t, record)
	if _, err := j.ReconcileR1(ReconcileRequest{OperationID: id, NamespaceSHA256: strings.Repeat("f", 64), ExpectedRevision: revision}, e); !errors.Is(err, ErrInvalid) {
		t.Fatalf("wrong namespace accepted: %v", err)
	}
	if _, err := j.ReconcileR1(ReconcileRequest{OperationID: id, NamespaceSHA256: ns, ExpectedRevision: revision - 1}, e); !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("stale CAS accepted: %v", err)
	}
}

func TestReconcileR1_shouldReturnIdempotentResolutionForOriginalCAS(t *testing.T) {
	j, id, revision, _, record := r1JournalFixture(t)
	ns, _ := j.NamespaceID()
	e := r1EvidenceFor(t, record)
	first, err := j.ReconcileR1(ReconcileRequest{OperationID: id, NamespaceSHA256: ns, ExpectedRevision: revision}, e)
	if err != nil {
		t.Fatal(err)
	}
	second, err := j.ReconcileR1(ReconcileRequest{OperationID: id, NamespaceSHA256: ns, ExpectedRevision: revision, ExpectedDigest: first.OriginalDigest}, R1Evidence{})
	if err != nil || second.Resolution == nil || second.Revision != first.Revision || second.Resolution.OriginalRecordSHA256 != first.Resolution.OriginalRecordSHA256 {
		t.Fatalf("not idempotent: %+v %v", second, err)
	}
	if _, err := j.ReconcileR1(ReconcileRequest{OperationID: id, NamespaceSHA256: ns, ExpectedRevision: revision - 1, ExpectedDigest: first.OriginalDigest}, R1Evidence{}); !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("stale repeated revision accepted: %v", err)
	}
	if _, err := j.ReconcileR1(ReconcileRequest{OperationID: id, NamespaceSHA256: ns, ExpectedRevision: revision, ExpectedDigest: strings.Repeat("f", 64)}, R1Evidence{}); !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("stale repeated digest accepted: %v", err)
	}
}

func TestReconcileR1_shouldRejectConcurrentLifecycleLock(t *testing.T) {
	j, id, revision, _, record := r1JournalFixture(t)
	unlock, err := filelock.Acquire(filepath.Join(j.Dir, "lifecycle.lock"))
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	ns, _ := j.NamespaceID()
	got, err := j.ReconcileR1(ReconcileRequest{OperationID: id, NamespaceSHA256: ns, ExpectedRevision: revision}, r1EvidenceFor(t, record))
	if !errors.Is(err, ErrReconcileBusy) || got.Disposition != DispositionBusy {
		t.Fatalf("concurrent lifecycle writer admitted: %+v %v", got, err)
	}
}

func TestReconcileR1_shouldNotCreateDatabaseOrProfile(t *testing.T) {
	j := Journal{Dir: filepath.Join(t.TempDir(), "missing")}
	ns, _ := j.NamespaceID()
	_, err := j.ReconcileR1(ReconcileRequest{OperationID: strings.Repeat("1", 32), NamespaceSHA256: ns, ExpectedRevision: 1}, R1Evidence{})
	if !errors.Is(err, ErrUnknown) {
		t.Fatalf("missing journal error=%v", err)
	}
	if _, err := os.Stat(j.Dir); !os.IsNotExist(err) {
		t.Fatalf("reconcile created profile directory: %v", err)
	}
	if err := os.MkdirAll(j.Dir, 0700); err != nil {
		t.Fatal(err)
	}
	_, err = j.ReconcileR1(ReconcileRequest{OperationID: strings.Repeat("1", 32), NamespaceSHA256: ns, ExpectedRevision: 1}, R1Evidence{})
	if !errors.Is(err, ErrUnknown) {
		t.Fatalf("missing DB error=%v", err)
	}
	if _, err := os.Stat(filepath.Join(j.Dir, "operations.db")); !os.IsNotExist(err) {
		t.Fatalf("reconcile created database: %v", err)
	}
}
