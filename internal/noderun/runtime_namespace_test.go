package noderun

import (
	"context"
	"net"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/xallor-ai/xallor-remote/internal/ipc"
	"github.com/xallor-ai/xallor-remote/internal/protocol"
	"github.com/xallor-ai/xallor-remote/internal/runtimeowner"
)

func lifecycleTestNamespace(t *testing.T, profile string) string {
	t.Helper()
	id, err := (runtimeowner.Journal{Dir: filepath.Join(profile, "runtime", "lifecycle")}).NamespaceID()
	if err != nil {
		t.Fatal(err)
	}
	return id
}

// A request delivered to another valid owner must fail before worker admission;
// detecting a mismatched receipt after mutation would be too late.
func TestLifecycle_should_refuseWrongNamespace_beforeLaunchingWorker(t *testing.T) {
	for _, namespace := range []string{"", strings.Repeat("f", 64), "not-a-digest"} {
		t.Run(namespace, func(t *testing.T) {
			store := ownerTestStore(t)
			var launches atomic.Int32
			done := make(chan struct{})
			close(done)
			d := New(store, nil).WithLifecycleLauncher(func(context.Context, runtimeowner.Command) (runtimeowner.Attempt, error) {
				launches.Add(1)
				return lifecycleTestAttempt{done: done}, nil
			})
			defer d.Stop()
			a, b := net.Pipe()
			defer b.Close()
			_ = b.SetDeadline(time.Now().Add(3 * time.Second))
			go d.serveIPC(ipc.NewConn(a))
			client := ipc.NewConn(b)
			params := map[string]string{"operation_id": strings.Repeat("1", 32)}
			if namespace != "" {
				params["namespace_sha256"] = namespace
			}
			if err := ipc.Call(client, "wrong-profile", "runtime.rollback", params); err != nil {
				t.Fatal(err)
			}
			frame, err := client.ReadFrame()
			if err != nil || frame.OK == nil || *frame.OK || frame.Code != protocol.RuntimeProfileMismatch || launches.Load() != 0 {
				t.Fatal("wrong namespace reached worker admission", frame, err, launches.Load())
			}
		})
	}
}
