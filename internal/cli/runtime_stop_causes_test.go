package cli

import (
	"context"
	"errors"
	"net"
	"strings"
	"testing"

	"github.com/xallor-ai/xallor-remote/internal/ipc"
	"github.com/xallor-ai/xallor-remote/internal/job"
	"github.com/xallor-ai/xallor-remote/internal/release"
)

// Losing a stop acknowledgement and finding retained job debt are independent
// failures. A later observation must not hide the earlier dispatch uncertainty.
func TestPreparedStop_shouldRetainAckFailure_whenJobObservationAlsoFails(t *testing.T) {
	for _, tc := range []struct {
		name  string
		cause error
	}{{"raw-job-cause", job.ErrUncertain}, {"user-diagnostic", jobCleanupError(job.ErrUncertain)}} {
		t.Run(tc.name, func(t *testing.T) {
			client, server := net.Pipe()
			t.Cleanup(func() { _ = client.Close(); _ = server.Close() })
			served := make(chan struct{})
			go func() {
				defer close(served)
				_, _ = ipc.NewConn(server).ReadRequest()
				_ = server.Close()
			}()
			watch := &preparedWatch{}
			a := &runtimeStopAttempt{
				intent: release.StopIntent{Mode: "peer", AttemptID: strings.Repeat("a", 32), PeerPID: 123,
					ImageSHA256: strings.Repeat("b", 64), IdentityScope: "process_image"},
				raw: client, conn: ipc.NewConn(client), watch: watch,
				resources: func() (string, string, error) {
					return "released_under_lock", "unreleased_or_unknown", tc.cause
				},
			}
			observed, err := a.Run(context.Background())
			<-served
			if !errors.Is(err, release.ErrCleanupUncertain) || !errors.Is(err, job.ErrUncertain) || !strings.Contains(err.Error(), "stop acknowledgement missing") {
				t.Fatalf("lost one of the independent stop causes: %v", err)
			}
			if observed.Confirms(a.intent) || observed.Dispatch != "may_have_been_sent" || observed.Process != "exit_observed" || !watch.closed {
				t.Fatalf("uncertainty changed stop evidence: %+v", observed)
			}
		})
	}
}
