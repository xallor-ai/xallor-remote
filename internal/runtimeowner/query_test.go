package runtimeowner

import (
	"encoding/json"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/xallor-ai/xallor-remote/internal/ipc"
	"github.com/xallor-ai/xallor-remote/internal/protocol"
)

// Unknown observation and malformed/mutation messages never initialize a
// journal. An unknown receipt is not evidence that execution never happened.
func TestOperationQueryIsReadOnlyAndRejectsMutationMessages(t *testing.T) {
	for _, test := range []struct{ method, params, code string }{
		{"runtime.operation.status", `{"operation_id":"11111111111111111111111111111111"}`, protocol.RuntimeOperationUnknown},
		{"runtime.operation.status", `{"operation_id":"11111111111111111111111111111111","bundle":"ignored"}`, protocol.AgentError},
		{"runtime.install", `{"operation_id":"11111111111111111111111111111111"}`, protocol.AgentError},
	} {
		j := journal(t)
		frame := j.QueryOperation(ipc.Request{ID: "query", Method: test.method, Params: json.RawMessage(test.params)})
		if frame.OK == nil || *frame.OK || frame.Code != test.code {
			t.Fatal(frame)
		}
		if _, err := os.Stat(j.Dir); !os.IsNotExist(err) {
			t.Fatal("query created journal", err)
		}
	}
}

// Reconcile observation validates the caller's CAS coordinates, returns only
// unresolved owner-native evidence status, and leaves both record and schema
// unchanged.
func TestReconcileObservationIsStrictReadOnly(t *testing.T) {
	j := journal(t)
	tx, err := j.Admit(request("1"))
	if err != nil {
		t.Fatal(err)
	}
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
	var raw []byte
	var revision int64
	if err := db.QueryRow("SELECT record,revision FROM operations WHERE id=?", request("1").ID).Scan(&raw, &revision); err != nil {
		db.Close()
		t.Fatal(err)
	}
	db.Close()
	digest := digestRecord(raw)
	params := `{"operation_id":"` + request("1").ID + `","namespace_sha256":"` + ns + `","expected_revision":` + strconv.FormatInt(revision, 10) + `,"expected_record_sha256":"` + digest + `"}`
	frame := j.ReconcileObservation(ipc.Request{ID: "reconcile", Method: "runtime.operation.reconcile", Params: []byte(params)})
	if frame.OK == nil || !*frame.OK {
		t.Fatal(frame)
	}
	var result struct {
		Disposition string    `json:"disposition"`
		Blockers    []Blocker `json:"blockers"`
	}
	if err := json.Unmarshal(frame.Result, &result); err != nil {
		t.Fatal(err)
	}
	if result.Disposition != DispositionUnresolved || len(result.Blockers) != 1 || result.Blockers[0].Code != "native_owner_observation_required" {
		t.Fatalf("unexpected observation: %+v", result)
	}
	for _, params := range []string{
		`{"operation_id":"` + request("1").ID + `","namespace_sha256":"` + ns + `","expected_revision":1,"expected_record_sha256":"` + digest + `","extra":true}`,
		`{"operation_id":"` + request("1").ID + `","namespace_sha256":"` + strings.Repeat("f", 64) + `","expected_revision":1,"expected_record_sha256":"` + digest + `"}`,
		`{"operation_id":"` + request("1").ID + `","namespace_sha256":"` + ns + `","expected_revision":999999999,"expected_record_sha256":"` + digest + `"}`,
		`{"operation_id":"` + request("2").ID + `","namespace_sha256":"` + ns + `","expected_revision":1,"expected_record_sha256":"` + digest + `"}`,
	} {
		frame := j.ReconcileObservation(ipc.Request{ID: "bad", Method: "runtime.operation.reconcile", Params: []byte(params)})
		if frame.OK == nil || *frame.OK {
			t.Fatal(frame)
		}
	}
	if _, err := j.Query(request("1").ID); err != nil {
		t.Fatal("observation changed record", err)
	}
}
