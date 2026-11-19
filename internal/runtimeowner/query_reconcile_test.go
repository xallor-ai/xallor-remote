package runtimeowner

import (
	"testing"

	"github.com/xallor-ai/xallor-remote/internal/ipc"
)

// Observation requests must reject malformed/head-supplied execution fields
// without creating a profile or touching lifecycle state.
func TestReconcileObservation_shouldRejectMalformedWithoutMutation(t *testing.T) {
	j := Journal{Dir: t.TempDir() + "\\missing"}
	for _, params := range []string{
		`{"operation_id":"bad"}`,
		`{"operation_id":"11111111111111111111111111111111","namespace_sha256":"` + "zz" + `","expected_revision":1,"path":"C:\\x"}`,
	} {
		frame := j.ReconcileObservation(ipc.Request{ID: "r", Method: "runtime.operation.reconcile", Params: []byte(params)})
		if frame.OK == nil || *frame.OK {
			t.Fatalf("malformed observation accepted: %+v", frame)
		}
	}
}
