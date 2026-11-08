package protocol

import "testing"

// The matrix is stable, complete for every invokable operation, and does not
// silently advertise an OS sandbox or a retry guarantee for unknown results.
func TestPolicyMatrix_should_coverEveryOperation_withoutSandboxClaim(t *testing.T) {
	m := PolicyMatrix()
	if m.Version != 1 || m.Sandbox != "none" {
		t.Fatalf("unexpected matrix header: %+v", m)
	}
	want := []string{OpInfo, OpRead, OpWrite, OpExec, OpProcesses, OpCancel, OpHandoffSubmit, OpHandoffGet}
	if len(m.Operations) != len(want) {
		t.Fatalf("operations=%d want %d", len(m.Operations), len(want))
	}
	for i, row := range m.Operations {
		if row.Operation != want[i] || row.Authorization == "" || row.TargetBoundary == "" || row.Terminal == "" || row.Retry == "" {
			t.Fatalf("invalid row %d: %+v", i, row)
		}
	}
}
