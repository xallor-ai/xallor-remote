package runtimeowner

import (
	"encoding/json"
	"testing"

	"github.com/xallor-ai/xallor-remote/internal/ipc"
)

// Terminal outcome and writer exit cannot settle a retained activation whose
// receipt is missing/corrupt; valid receipts and empty terminal ledgers still settle.
func TestQuerySettled_shouldRequireValidResourceReceipt_afterTerminalWriterExit(t *testing.T) {
	for _, name := range []string{"valid", "missing", "corrupt", "live_writer", "no_resources"} {
		t.Run(name, func(t *testing.T) {
			var j Journal
			var tx *Transaction
			if name == "no_resources" {
				j = journal(t)
				var err error
				tx, err = j.Admit(request("1"))
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = tx.Close() })
				if err := tx.Finish(Rejected); err != nil {
					t.Fatal(err)
				}
			} else {
				var resource ResourceObligation
				var evidence SelectionOwnerEvidence
				j, tx, resource, evidence = selectionFixture(t, true)
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
				if name == "missing" || name == "corrupt" {
					var raw []byte
					if name == "corrupt" {
						raw = []byte(`{"invalid_receipt":true}`)
					}
					if _, err := tx.db.Exec("UPDATE resource_obligations SET selection_receipt=? WHERE operation_id=?", raw, tx.record.Request.ID); err != nil {
						t.Fatal(err)
					}
				}
			}
			if name != "live_writer" {
				if err := tx.Close(); err != nil {
					t.Fatal(err)
				}
			}
			want := name == "valid" || name == "no_resources"
			if _, settled, err := j.QuerySettled(request("1").ID); err != nil || settled != want {
				t.Errorf("QuerySettled settled=%v want=%v err=%v", settled, want, err)
			}
			frame := j.QueryOperation(ipc.Request{ID: "query", Method: "runtime.operation.status", Params: mustJSON(map[string]string{"operation_id": request("1").ID})})
			if frame.OK == nil || !*frame.OK {
				t.Fatalf("QueryOperation failed: %+v", frame)
			}
			var result struct {
				Settled bool `json:"settled"`
			}
			if err := json.Unmarshal(frame.Result, &result); err != nil {
				t.Fatal(err)
			}
			if result.Settled != want {
				t.Errorf("QueryOperation settled=%v want=%v", result.Settled, want)
			}
		})
	}
}
