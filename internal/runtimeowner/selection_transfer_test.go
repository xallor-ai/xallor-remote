package runtimeowner

import (
	"encoding/json"
	"fmt"
	"github.com/xallor-ai/xallor-remote/internal/release"
	"strings"
	"testing"
)

func selectionFixture(t *testing.T, ready bool) (Journal, *Transaction, ResourceObligation, SelectionOwnerEvidence) {
	t.Helper()
	j := journal(t)
	tx, err := j.Admit(request("1"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tx.Close() })
	bindFixture(t, tx, release.ObserveSelection(release.State{}), strings.Repeat("b", 64))
	i := resourceIntentFixture("1")
	i.Kind = ResourceKindActivation
	i.ActivationID = strings.Repeat("1", 32)
	i.LaunchID = strings.Repeat("2", 32)
	i.ImageSHA256 = strings.Repeat("b", 64)
	if err := tx.RecordResourceIntent(i); err != nil {
		t.Fatal(err)
	}
	prefixFixture(t, tx, "ready")
	if ready {
		if err := testApply(tx, "ready", i.ImageSHA256, func() error { return nil }); err != nil {
			t.Fatal(err)
		}
	}
	e := SelectionOwnerEvidence{Current: r1EvidenceFor(t, tx.record).Current, SourceSnapshotSHA256: i.SourceSnapshotSHA256}
	return j, tx, i, e
}

// Missing or corrupt retained receipts never settle completion or reconciliation debt.
func TestSelectionTransfer_shouldRejectRetainedLabels_withoutExactPersistedReceipt(t *testing.T) {
	for _, name := range []string{"missing", "digest", "revision", "unknown_field", "oversized", "resource"} {
		t.Run(name, func(t *testing.T) {
			j, tx, i, e := selectionFixture(t, true)
			receipt, err := tx.SelectionReceipt(i, 1, e)
			if err != nil {
				t.Fatal(err)
			}
			if err := tx.TransferSelectedResource(receipt); err != nil {
				t.Fatal(err)
			}
			var raw []byte
			switch name {
			case "missing":
				raw = nil
			case "digest":
				receipt.data.RecordSHA256 = strings.Repeat("f", 64)
				raw, _ = json.Marshal(receipt.data)
			case "revision":
				receipt.data.RecordRevision = tx.revision
				raw, _ = json.Marshal(receipt.data)
			case "resource":
				receipt.data.Resource.Generation++
				raw, _ = json.Marshal(receipt.data)
			case "unknown_field":
				raw = []byte(`{"unknown":true}`)
			case "oversized":
				raw = []byte(`{"padding":"` + strings.Repeat("x", maxSelectionReceiptBytes) + `"}`)
			}
			_, err = tx.db.Exec("UPDATE resource_obligations SET selection_receipt=? WHERE operation_id=? AND resource_id=?", raw, tx.record.Request.ID, i.ID)
			if name == "oversized" {
				if err == nil {
					t.Fatal("oversized receipt accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if !j.HasUnsettledResources(tx.record.Request.ID) {
				t.Fatal("malformed receipt settled debt")
			}
			if err := tx.Finish(Succeeded); err == nil {
				t.Fatal("malformed receipt allowed finish")
			}
		})
	}
}

// Every old schema is refused, with no in-place upgrade.
func TestSelectionTransfer_shouldRefuseSchemas1Through7(t *testing.T) {
	for version := 1; version <= 7; version++ {
		t.Run(fmt.Sprint(version), func(t *testing.T) {
			j, tx, _, _ := selectionFixture(t, true)
			if _, err := tx.db.Exec(fmt.Sprintf("PRAGMA user_version=%d", version)); err != nil {
				t.Fatal(err)
			}
			if _, err := j.Resources(tx.record.Request.ID); err == nil {
				t.Fatal("old schema accepted")
			}
		})
	}
}

// An intervening durable operation revision prevents transfer and rolls back the resource CAS.
func TestSelectionTransfer_shouldRollbackResource_whenOperationCASIsStale(t *testing.T) {
	j, tx, i, e := selectionFixture(t, true)
	receipt, err := tx.SelectionReceipt(i, 1, e)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.db.Exec("UPDATE operations SET revision=revision+1 WHERE id=?", tx.record.Request.ID); err != nil {
		t.Fatal(err)
	}
	if err := tx.TransferSelectedResource(receipt); err == nil {
		t.Fatal("stale operation accepted")
	}
	rows, err := j.Resources(tx.record.Request.ID)
	if err != nil || !selectionResourceRowsMatch(rows, i, ResourceAllocated) {
		t.Fatal(rows, err)
	}
}

// A valid baseline restoration is not the requested candidate's acknowledgement.
func TestSelectionTransfer_shouldRejectValidBaselineRestoration(t *testing.T) {
	r := lifecycleFixture(t, strings.Repeat("a", 64), true)
	if !r.valid() || !r.restored() {
		t.Fatal("invalid restore fixture")
	}
	if requestedSelectionAcknowledged(r, r1EvidenceFor(t, r).Current) {
		t.Fatal("baseline restoration accepted")
	}
}

// No caller can reconstruct the transaction owner token from serialized data.
func TestSelectionTransfer_shouldRejectReceipt_withoutOwningTransaction(t *testing.T) {
	_, tx, i, e := selectionFixture(t, true)
	receipt, err := tx.SelectionReceipt(i, 1, e)
	if err != nil {
		t.Fatal(err)
	}
	receipt.owner = nil
	if err := tx.TransferSelectedResource(receipt); err == nil {
		t.Fatal("unowned receipt accepted")
	}
}

// Transfer settles only the resource row and preserves original operation bytes.
func TestSelectionTransfer_shouldRetainExactSelectedActivation_withoutChangingRecord(t *testing.T) {
	j, tx, i, e := selectionFixture(t, true)
	before := string(tx.last)
	receipt, err := tx.SelectionReceipt(i, 1, e)
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.TransferSelectedResource(receipt); err != nil {
		t.Fatal(err)
	}
	if string(tx.last) != before {
		t.Fatal("original record changed")
	}
	rows, err := j.Resources(tx.record.Request.ID)
	if err != nil || !selectionResourceRowsMatch(rows, i, ResourceRetained) {
		t.Fatal(rows, err)
	}
	if j.HasUnsettledResources(tx.record.Request.ID) {
		t.Fatal("valid retention left unsettled")
	}
	if err := tx.TransferSelectedResource(receipt); err == nil {
		t.Fatal("duplicate accepted")
	}
	if err := tx.Finish(Succeeded); err != nil {
		t.Fatal(err)
	}
	if j.HasUnsettledResources(tx.record.Request.ID) {
		t.Fatal("finish invalidated receipt")
	}
}

// Identity and readiness observations are independent gates, including same-SHA substitutions.
func TestSelectionTransfer_shouldRejectWrongOwnerOrUnready(t *testing.T) {
	for _, name := range []string{"activation", "launch", "image", "manifest", "source", "peer", "unauthenticated", "unstable", "selection", "pre_ready"} {
		t.Run(name, func(t *testing.T) {
			_, tx, i, e := selectionFixture(t, name != "pre_ready")
			switch name {
			case "activation":
				e.Current.ActivationID = strings.Repeat("f", 32)
			case "launch":
				e.Current.LaunchID = strings.Repeat("f", 32)
			case "image":
				e.Current.LoadedImageSHA256 = strings.Repeat("f", 64)
			case "manifest":
				e.Current.SourceManifestSHA256 = strings.Repeat("f", 64)
			case "source":
				e.SourceSnapshotSHA256 = strings.Repeat("f", 64)
			case "peer":
				e.Current.PeerPID = 0
			case "unauthenticated":
				e.Current.OwnerAuthenticated = false
			case "unstable":
				e.Current.ReadyStable = false
			case "selection":
				e.Current.Selection.StateSHA256 = strings.Repeat("f", 64)
			}
			if _, err := tx.SelectionReceipt(i, 1, e); err == nil {
				t.Fatal("invalid evidence minted receipt")
			}
		})
	}
}

// Both operation and resource CAS must reject stale observations without settling debt.
func TestSelectionTransfer_shouldRejectStaleAndAlteredReceipt(t *testing.T) {
	for _, name := range []string{"revision", "resource_revision", "operation", "namespace", "digest", "attempt", "generation", "purpose", "restore", "cancel", "closed"} {
		t.Run(name, func(t *testing.T) {
			j, tx, i, e := selectionFixture(t, true)
			receipt, err := tx.SelectionReceipt(i, 1, e)
			if err != nil {
				t.Fatal(err)
			}
			switch name {
			case "revision":
				receipt.data.RecordRevision++
			case "resource_revision":
				receipt.data.ResourceRevision++
			case "operation":
				receipt.data.OperationID = strings.Repeat("f", 32)
			case "namespace":
				receipt.data.NamespaceSHA256 = strings.Repeat("f", 64)
			case "digest":
				receipt.data.RecordSHA256 = strings.Repeat("f", 64)
			case "attempt":
				receipt.data.Resource.AttemptID = strings.Repeat("f", 32)
			case "generation":
				receipt.data.Resource.Generation++
			case "purpose":
				receipt.data.Purpose = "restore"
			case "restore", "cancel":
				tx.record.Effects[len(tx.record.Effects)-1].Purpose = name
			case "closed":
				_ = tx.Close()
			}
			if err := tx.TransferSelectedResource(receipt); err == nil {
				t.Fatal("invalid transfer accepted")
			}
			if !j.HasUnsettledResources(i.ID) {
				t.Fatal("invalid transfer settled debt")
			}
		})
	}
}

// Selection fixtures also own the exact closed stop group; transfer must leave
// that independent receipt intact while changing only the selected activation.
func selectionResourceRowsMatch(rows []ResourceRecord, activation ResourceObligation, status string) bool {
	if len(rows) != 2 {
		return false
	}
	found := false
	for _, r := range rows {
		if r.ID == activation.ID {
			expected := activation
			if status == ResourceRetained {
				expected.Retention = ResourceRetainSelected
			}
			if r.ResourceObligation != expected || r.Status != status {
				return false
			}
			found = true
		} else if r.Kind != ResourceKindObserver || r.Status != ResourceClosed || r.Cause != CauseClosed || r.Revision != 2 {
			return false
		}
	}
	return found
}
