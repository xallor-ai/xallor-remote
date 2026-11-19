package noderun

import (
	"encoding/json"
	"net"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/xallor-ai/xallor-remote/internal/ipc"
	"github.com/xallor-ai/xallor-remote/internal/runtimeowner"
)

// Transaction observation must remain available before serving readiness and
// must return the recorded outcome rather than infer success from an IPC reply.
func TestRuntimeOperationQueryIsAvailableWhileStartupIsPending(t *testing.T) {
	store := ownerTestStore(t)
	j := runtimeowner.Journal{Dir: filepath.Join(store.Dir, "runtime", "lifecycle")}
	id := strings.Repeat("1", 32)
	tx, err := j.Admit(runtimeowner.Request{ID: id, Kind: "install", IntentSHA256: strings.Repeat("a", 64)})
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Close()
	d := New(store, nil).WithStartupGate(func() (bool, error) { return false, nil })
	defer d.Stop()
	a, b := net.Pipe()
	defer b.Close()
	b.SetDeadline(time.Now().Add(3 * time.Second))
	go d.serveIPC(ipc.NewConn(a))
	client := ipc.NewConn(b)
	if err := ipc.Call(client, "query", "runtime.operation.status", map[string]string{"operation_id": id}); err != nil {
		t.Fatal(err)
	}
	frame, err := client.ReadFrame()
	if err != nil || frame.OK == nil || !*frame.OK {
		t.Fatal(frame, err)
	}
	var result struct {
		Operation runtimeowner.Record `json:"operation"`
	}
	if err := json.Unmarshal(frame.Result, &result); err != nil {
		t.Fatal(err)
	}
	if result.Operation.Outcome != runtimeowner.Pending || result.Operation.Request.ID != id {
		t.Fatal(result)
	}
}
