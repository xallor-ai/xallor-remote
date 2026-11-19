package runtimeowner

import (
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/xallor-ai/xallor-remote/internal/release"
)

func r4NotDispatchedStop() release.StopObservation {
	o := releasedStopObservation()
	o.Dispatch, o.Process, o.RuntimeResource, o.Jobs, o.Result = "not_sent", "not_observed", "released_under_lock", "empty", "not_dispatched"
	return o
}
func r4Request(id string) Request {
	return Request{ID: strings.Repeat(id, 32), Kind: "rollback", IntentSHA256: strings.Repeat("a", 64)}
}

func r4Fixture(t *testing.T) (Journal, *Transaction) {
	t.Helper()
	j := journal(t)
	tx, err := j.Admit(r4Request("1"))
	if err != nil {
		t.Fatal(err)
	}
	base := boundIntent("prepare", strings.Repeat("b", 64)).Before
	base.Current = boundIntent("ready", strings.Repeat("c", 64)).Target
	base.Pending = boundIntent("prepare", strings.Repeat("b", 64)).Target
	base.StateSHA256 = strings.Repeat("d", 64)
	bindFixture(t, tx, base, strings.Repeat("c", 64))
	if err := testStop(tx, func() (release.StopObservation, error) {
		return r4NotDispatchedStop(), errors.New("preflight rejected")
	}); err == nil {
		t.Fatal("missing pre-dispatch failure")
	}
	if err := tx.Finish(Failed); err != nil {
		t.Fatal(err)
	}
	return j, tx
}

// R4 appends a resolution while retaining immutable original operation bytes.
func TestReconcileR4_shouldAppendNotAppliedResolution_withoutMutatingOriginal(t *testing.T) {
	j, tx := r4Fixture(t)
	if err := tx.Close(); err != nil {
		t.Fatal(err)
	}
	id := r4Request("1").ID
	db, err := j.open(false)
	if err != nil {
		t.Fatal(err)
	}
	var before []byte
	if err := db.QueryRow("SELECT record FROM operations WHERE id=?", id).Scan(&before); err != nil {
		db.Close()
		t.Fatal(err)
	}
	db.Close()
	ns, _ := j.NamespaceID()
	got, err := j.Reconcile(ReconcileRequest{OperationID: id, NamespaceSHA256: ns, ExpectedRevision: tx.revision}, ReconciliationEvidence{AdmissionHeld: true, Resources: []ResourceEvidence{{Kind: "jobs", State: "absent"}, {Kind: "runtime", State: "released"}, {Kind: "observers", State: "released"}}})
	if err != nil || got.Resolution == nil || got.Resolution.RequestResult != RequestNotApplied || got.Resolution.Disposition != DispositionResolved {
		t.Fatalf("reconcile=%+v err=%v", got, err)
	}
	db, err = j.open(false)
	if err != nil {
		t.Fatal(err)
	}
	var after []byte
	if err := db.QueryRow("SELECT record FROM operations WHERE id=?", id).Scan(&after); err != nil {
		db.Close()
		t.Fatal(err)
	}
	db.Close()
	if string(before) != string(after) {
		t.Fatal("resolution mutated immutable original record")
	}
	if _, err := os.Stat(j.Dir); err != nil {
		t.Fatal(err)
	}
}

func TestReconcileR4_shouldRefuseMissingClosureEvidence(t *testing.T) {
	j, tx := r4Fixture(t)
	tx.Close()
	ns, _ := j.NamespaceID()
	got, err := j.Reconcile(ReconcileRequest{OperationID: r4Request("1").ID, NamespaceSHA256: ns, ExpectedRevision: tx.revision}, ReconciliationEvidence{AdmissionHeld: true})
	if err != nil || got.Resolution != nil || got.Disposition != DispositionUnresolved {
		t.Fatalf("missing evidence was resolved: %+v err=%v", got, err)
	}
}

func TestReconcileR4_shouldRejectWrongNamespaceAndStaleCAS(t *testing.T) {
	j, tx := r4Fixture(t)
	tx.Close()
	ns, _ := j.NamespaceID()
	id := r4Request("1").ID
	if _, err := j.Reconcile(ReconcileRequest{OperationID: id, NamespaceSHA256: strings.Repeat("f", 64), ExpectedRevision: tx.revision}, ReconciliationEvidence{AdmissionHeld: true}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("wrong namespace accepted: %v", err)
	}
	if _, err := j.Reconcile(ReconcileRequest{OperationID: id, NamespaceSHA256: ns, ExpectedRevision: tx.revision - 1}, ReconciliationEvidence{AdmissionHeld: true}); !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("stale CAS accepted: %v", err)
	}
}

func TestReconcileR4_shouldReturnIdempotentResolution(t *testing.T) {
	j, tx := r4Fixture(t)
	tx.Close()
	ns, _ := j.NamespaceID()
	id := r4Request("1").ID
	e := ReconciliationEvidence{AdmissionHeld: true, Resources: []ResourceEvidence{{Kind: "jobs", State: "absent"}, {Kind: "runtime", State: "released"}, {Kind: "observers", State: "released"}}}
	first, err := j.Reconcile(ReconcileRequest{OperationID: id, NamespaceSHA256: ns, ExpectedRevision: tx.revision}, e)
	if err != nil {
		t.Fatal(err)
	}
	second, err := j.Reconcile(ReconcileRequest{OperationID: id, NamespaceSHA256: ns, ExpectedRevision: tx.revision, ExpectedDigest: first.OriginalDigest}, ReconciliationEvidence{AdmissionHeld: true})
	if err != nil || second.Resolution == nil || second.Resolution.OriginalRecordSHA256 != first.Resolution.OriginalRecordSHA256 {
		t.Fatalf("not idempotent: %+v %v", second, err)
	}
}

func TestReconcileR4_shouldRemainUnresolvedWhileWorkerActive(t *testing.T) {
	j, tx := r4Fixture(t)
	tx.Close()
	ns, _ := j.NamespaceID()
	got, err := j.Reconcile(ReconcileRequest{OperationID: r4Request("1").ID, NamespaceSHA256: ns, ExpectedRevision: tx.revision}, ReconciliationEvidence{AdmissionHeld: true, WorkerActive: true, Resources: []ResourceEvidence{{Kind: "jobs", State: "absent"}, {Kind: "runtime", State: "released"}, {Kind: "observers", State: "released"}}})
	if err != nil || got.Disposition != DispositionUnresolved || got.Resolution != nil {
		t.Fatalf("active worker was resolved: %+v %v", got, err)
	}
}
