package noderun

import (
	"errors"
	"testing"

	"github.com/xallor-ai/xallor-remote/internal/job"
	"github.com/xallor-ai/xallor-remote/internal/process"
	"github.com/xallor-ai/xallor-remote/internal/protocol"
)

// Admission and release uncertainty must remain structured so CLI/MCP callers
// cannot mistake a rejected retry or an unconfirmed cleanup for success.
func TestExecutionResourceCode_should_preserveUncertainAndUnavailableStates(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want string
	}{
		{"capacity", job.ErrCapacity, protocol.QuotaExceeded},
		{"uncertain", job.ErrUncertain, protocol.ResourceUncertain},
		{"unavailable", job.ErrUnavailable, protocol.ResourceUnavailable},
		{"cleanup", process.ErrCleanup, protocol.ResourceUncertain},
		{"other", errors.New("other"), protocol.AgentError},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.name == "capacity" {
				if got := executionAdmissionCode(tc.err); got != tc.want {
					t.Fatal(got)
				}
				return
			}
			if got := executionResourceCode(tc.err); got != tc.want {
				t.Fatal(got)
			}
		})
	}
}
