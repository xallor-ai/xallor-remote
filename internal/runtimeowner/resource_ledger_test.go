package runtimeowner

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

func resourceIntentFixture(id string) ResourceObligation {
	return ResourceObligation{ID: strings.Repeat(id, 32), Phase: ResourcePhasePreflight, Kind: ResourceKindSourceSnapshot, AttemptID: strings.Repeat("a", 32), Generation: 1, ActivationID: strings.Repeat("b", 32), LaunchID: strings.Repeat("c", 32), ImageSHA256: strings.Repeat("d", 64), SourceSnapshotSHA256: strings.Repeat("e", 64), ObserverScope: ObserverScopeOwnerProcess, Retention: ResourceReleaseRequired}
}

// A resource intent is durable before allocation and remains active after a worker exit.
func TestResourceLedger_shouldRetainAllocatedDebt_afterTransactionClose(t *testing.T) {
	j := journal(t)
	tx, err := j.Admit(request("1"))
	if err != nil {
		t.Fatal(err)
	}
	intent := resourceIntentFixture("1")
	if err := tx.RecordResourceIntent(intent); err != nil {
		t.Fatal(err)
	}
	if err := tx.Close(); err != nil {
		t.Fatal(err)
	}
	resources, err := j.Resources(request("1").ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(resources) != 1 || resources[0].Status != ResourceAllocated {
		t.Fatalf("resources=%+v", resources)
	}
	if !j.HasUnsettledResources(request("1").ID) {
		t.Fatal("allocated release debt was treated as settled")
	}
}

// A terminal receipt cannot erase an unreleased owner obligation.
func TestResourceLedger_shouldKeepOperationActive_whenFinishHasUnreleasedDebt(t *testing.T) {
	j := journal(t)
	tx, err := j.Admit(request("1"))
	if err != nil {
		t.Fatal(err)
	}
	intent := resourceIntentFixture("1")
	if err := tx.RecordResourceIntent(intent); err != nil {
		t.Fatal(err)
	}
	if err := tx.Finish(Rejected); !errors.Is(err, ErrUncertain) {
		t.Fatalf("finish erased debt: %v", err)
	}
	_ = tx.Close()
	if _, err := j.Admit(request("2")); !errors.Is(err, ErrPending) {
		t.Fatalf("unreleased debt did not keep journal active: %v", err)
	}
}

// Retention needs a future authoritative selected receipt, not an intent label.
func TestResourceLedger_shouldRefuseSelectedRetention_withoutSelectedReceipt(t *testing.T) {
	j := journal(t)
	tx, err := j.Admit(request("1"))
	if err != nil {
		t.Fatal(err)
	}
	intent := resourceIntentFixture("1")
	intent.Kind = ResourceKindActivation
	intent.Retention = ResourceRetainSelected
	if err := tx.RecordResourceIntent(intent); !errors.Is(err, ErrInvalid) {
		t.Fatal("retention without selection receipt accepted", err)
	}
	_ = tx.Close()
}

// Retention cannot relabel a source or probe as an intentionally selected activation.
func TestResourceLedger_shouldRejectSelectedRetention_forNonActivation(t *testing.T) {
	j := journal(t)
	tx, err := j.Admit(request("1"))
	if err != nil {
		t.Fatal(err)
	}
	intent := resourceIntentFixture("1")
	intent.Retention = ResourceRetainSelected
	if err := tx.RecordResourceIntent(intent); !errors.Is(err, ErrInvalid) {
		t.Fatalf("non-activation retention err=%v", err)
	}
	_ = tx.Close()
}

// Closure is identity and revision bound; duplicate and mismatched operation updates are refused.
func TestResourceLedger_shouldRejectDuplicateAndWrongIdentity_updates(t *testing.T) {
	j := journal(t)
	tx, err := j.Admit(request("1"))
	if err != nil {
		t.Fatal(err)
	}
	intent := resourceIntentFixture("1")
	if err := tx.RecordResourceIntent(intent); err != nil {
		t.Fatal(err)
	}
	if err := tx.RecordResourceIntent(intent); !errors.Is(err, ErrResourceConflict) {
		t.Fatalf("duplicate err=%v", err)
	}
	wrong := intent
	wrong.AttemptID = strings.Repeat("f", 32)
	if err := tx.CloseResource(intent.ID, wrong, ResourceClosure{Cause: CauseClosed}); !errors.Is(err, ErrResourceConflict) {
		t.Fatalf("wrong identity err=%v", err)
	}
	wrong = intent
	wrong.Generation = 2
	if err := tx.CloseResource(intent.ID, wrong, ResourceClosure{Cause: CauseClosed}); !errors.Is(err, ErrResourceConflict) {
		t.Fatalf("wrong generation err=%v", err)
	}
	if err := tx.CloseResource(intent.ID, intent, ResourceClosure{Cause: CauseClosed}); err != nil {
		t.Fatal(err)
	}
	if err := tx.CloseResource(intent.ID, intent, ResourceClosure{Cause: CauseClosed}); !errors.Is(err, ErrResourceConflict) {
		t.Fatalf("repeat close err=%v", err)
	}
	_ = tx.Close()
}

// The bounded table rejects a thirty-third obligation before any allocation.
func TestResourceLedger_shouldBoundObligationRows(t *testing.T) {
	j := journal(t)
	tx, err := j.Admit(request("1"))
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < maxResourceRows; i++ {
		intent := resourceIntentFixture("1")
		intent.ID = fmt.Sprintf("%032x", i+1)
		if err := tx.RecordResourceIntent(intent); err != nil {
			t.Fatalf("row %d: %v", i, err)
		}
	}
	intent := resourceIntentFixture("1")
	intent.ID = strings.Repeat("f", 32)
	if err := tx.RecordResourceIntent(intent); !errors.Is(err, ErrInvalid) {
		t.Fatalf("row bound err=%v", err)
	}
	_ = tx.Close()
}

// Unknown cleanup is durable and remains debt; no close is inferred from process exit.
func TestResourceLedger_shouldKeepUnknownCleanup_asActiveDebt(t *testing.T) {
	j := journal(t)
	tx, err := j.Admit(request("1"))
	if err != nil {
		t.Fatal(err)
	}
	intent := resourceIntentFixture("1")
	if err := tx.RecordResourceIntent(intent); err != nil {
		t.Fatal(err)
	}
	if err := tx.MarkResourceUnknown(intent.ID, intent, CauseOwnerLost); err != nil {
		t.Fatal(err)
	}
	if err := tx.Close(); err != nil {
		t.Fatal(err)
	}
	got, err := j.Resources(request("1").ID)
	if err != nil || len(got) != 1 {
		t.Fatal(got, err)
	}
	if got[0].Status != ResourceUnknown || !j.HasUnsettledResources(request("1").ID) {
		t.Fatalf("got=%+v", got)
	}
}

// Resource records remain bounded independently of the operation JSON row.
func TestResourceLedger_shouldRejectOversizedBoundedFields(t *testing.T) {
	j := journal(t)
	tx, err := j.Admit(request("1"))
	if err != nil {
		t.Fatal(err)
	}
	intent := resourceIntentFixture("1")
	intent.ObserverScope = strings.Repeat("x", maxResourceDetail+1)
	if err := tx.RecordResourceIntent(intent); !errors.Is(err, ErrInvalid) {
		t.Fatalf("oversized intent err=%v", err)
	}
	_ = tx.Close()
}

// Old schema versions are never upgraded in place.
func TestResourceLedger_shouldRefuseSchema6(t *testing.T) {
	j := journal(t)
	tx, err := j.Admit(request("1"))
	if err != nil {
		t.Fatal(err)
	}
	_ = tx.Close()
	db, err := j.open(true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("PRAGMA user_version=6"); err != nil {
		t.Fatal(err)
	}
	_ = db.Close()
	if _, err := j.Resources(request("1").ID); !errors.Is(err, ErrInvalid) {
		t.Fatalf("schema6 err=%v", err)
	}
}

// Owner reconciliation cannot accept caller-settled evidence while durable release debt remains.
func TestResourceLedger_shouldBlockR1Resolution_withAllocatedDebt(t *testing.T) {
	j, id, revision, _, record := r1JournalFixture(t)
	ns, err := j.NamespaceID()
	if err != nil {
		t.Fatal(err)
	}
	intent := resourceIntentFixture("1")
	db, err := j.openExistingWritable()
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`INSERT INTO resource_obligations(operation_id,namespace_sha256,resource_id,phase,kind,status,cause,attempt_id,generation,activation_id,launch_id,image_sha256,source_snapshot_sha256,observer_scope,retention,revision) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,1)`, id, ns, intent.ID, intent.Phase, intent.Kind, ResourceAllocated, CauseIntentRecorded, intent.AttemptID, intent.Generation, intent.ActivationID, intent.LaunchID, intent.ImageSHA256, intent.SourceSnapshotSHA256, intent.ObserverScope, intent.Retention)
	_ = db.Close()
	if err != nil {
		t.Fatal(err)
	}
	got, err := j.ReconcileR1(ReconcileRequest{OperationID: id, NamespaceSHA256: ns, ExpectedRevision: revision}, r1EvidenceFor(t, record))
	if err != nil || got.Resolution != nil || got.Disposition != DispositionUnresolved {
		t.Fatalf("allocated debt was reconciled: %+v err=%v", got, err)
	}
}
