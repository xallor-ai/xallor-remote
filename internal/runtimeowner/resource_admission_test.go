package runtimeowner

import (
	"errors"
	"strings"
	"testing"
)

// A terminal flag cannot authorize the next operation while a retained receipt
// is malformed or any prior resource is allocated, unknown, or orphaned.
func TestResourceAdmission_shouldFailClosed_whenPriorTerminalResourceDebtRemains(t *testing.T) {
	for _, name := range []string{"valid", "missing", "corrupt", "allocated", "unknown", "orphan", "empty_orphan"} {
		t.Run(name, func(t *testing.T) {
			j, tx, resource, evidence := selectionFixture(t, true)
			receipt, err := tx.SelectionReceipt(resource, 1, evidence)
			if err != nil {
				t.Fatal(err)
			}
			if err := tx.TransferSelectedResource(receipt); err != nil {
				t.Fatal(err)
			}
			if err := tx.Finish(Succeeded); err != nil {
				t.Fatal(err)
			}
			switch name {
			case "missing":
				_, err = tx.db.Exec("UPDATE resource_obligations SET selection_receipt=NULL")
			case "corrupt":
				_, err = tx.db.Exec(`UPDATE resource_obligations SET selection_receipt='{}'`)
			case "allocated", "unknown":
				_, err = tx.db.Exec("UPDATE resource_obligations SET status=?,retention=?,selection_receipt=NULL", name, ResourceReleaseRequired)
			case "orphan":
				_, err = tx.db.Exec("UPDATE resource_obligations SET operation_id=?", request("f").ID)
			case "empty_orphan":
				_, err = tx.db.Exec("UPDATE resource_obligations SET operation_id=''")
			}
			if err != nil {
				t.Fatal(err)
			}
			if err := tx.Close(); err != nil {
				t.Fatal(err)
			}
			next, err := j.Admit(request("2"))
			if next != nil {
				_ = next.Close()
			}
			if name == "valid" {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			if !errors.Is(err, ErrPending) {
				t.Fatalf("admitted over %s debt: %v", name, err)
			}
		})
	}
}

func closedResourceOperation(t *testing.T, j Journal, id string) {
	t.Helper()
	tx, err := j.Admit(request(id))
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Close()
	resource := resourceIntentFixture(id)
	if err := tx.RecordResourceIntent(resource); err != nil {
		t.Fatal(err)
	}
	if err := tx.CloseResource(resource.ID, resource, ResourceClosure{Cause: CauseClosed}); err != nil {
		t.Fatal(err)
	}
	if err := tx.Finish(Rejected); err != nil {
		t.Fatal(err)
	}
}

// Closed resources must pass the same strict identity validation as retained
// rows, including a later historical operation after an earlier valid page.
func TestResourceAdmission_shouldValidateClosedRows_acrossHistoricalOperations(t *testing.T) {
	for _, name := range []string{"valid_closed", "wrong_namespace", "wrong_cause", "wrong_identity", "closed_orphan", "later_bad_operation"} {
		t.Run(name, func(t *testing.T) {
			j := journal(t)
			closedResourceOperation(t, j, "1")
			id := request("1").ID
			if name == "later_bad_operation" {
				closedResourceOperation(t, j, "2")
				id = request("2").ID
			}
			db, err := j.openExistingWritable()
			if err != nil {
				t.Fatal(err)
			}
			switch name {
			case "wrong_namespace", "later_bad_operation":
				_, err = db.Exec("UPDATE resource_obligations SET namespace_sha256=? WHERE operation_id=?", strings.Repeat("f", 64), id)
			case "wrong_cause":
				_, err = db.Exec("UPDATE resource_obligations SET cause='invalid' WHERE operation_id=?", id)
			case "wrong_identity":
				_, err = db.Exec("UPDATE resource_obligations SET attempt_id=? WHERE operation_id=?", strings.Repeat("z", 32), id)
			case "closed_orphan":
				_, err = db.Exec("UPDATE resource_obligations SET operation_id=? WHERE operation_id=?", request("f").ID, id)
			}
			_ = db.Close()
			if err != nil {
				t.Fatal(err)
			}
			next, err := j.Admit(request("3"))
			if next != nil {
				_ = next.Close()
			}
			if name == "valid_closed" {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			if !errors.Is(err, ErrPending) {
				t.Fatalf("admitted over malformed closed row %s: %v", name, err)
			}
		})
	}
}
