package cli

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

// The CLI head sends only the observation contract over IPC and does not send
// a bundle, journal path, or readiness/bootstrap request.
func TestRuntimeReconcileCLI_shouldSendOnlyObservationFields(t *testing.T) {
	profile := t.TempDir()
	t.Setenv("XALLOR_REMOTE_DATA_DIR", profile)
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	id := strings.Repeat("1", 32)
	digest := strings.Repeat("a", 64)
	ns, err := (runtimeowner.Journal{Dir: filepath.Join(profile, "runtime", "lifecycle")}).NamespaceID()
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		conn := ipc.NewConn(server)
		req, err := conn.ReadRequest()
		if err != nil {
			t.Errorf("read request: %v", err)
			return
		}
		if req.Method != "runtime.operation.reconcile" {
			t.Errorf("method=%q", req.Method)
		}
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(req.Params, &fields); err != nil {
			t.Errorf("params: %v", err)
		} else if len(fields) != 4 || fields["operation_id"] == nil || fields["namespace_sha256"] == nil || fields["expected_revision"] == nil || fields["expected_record_sha256"] == nil {
			t.Errorf("unexpected fields: %v", fields)
		} else {
			var gotNamespace string
			if json.Unmarshal(fields["namespace_sha256"], &gotNamespace) != nil || gotNamespace != ns {
				t.Errorf("namespace=%q want %q", gotNamespace, ns)
			}
		}
		_ = conn.WriteFrame(ipc.OK(req.ID, map[string]any{"disposition": "unresolved"}))
	}()
	cmd := cmdRuntimeReconcileWith(func(time.Duration) (net.Conn, error) { return client, nil })
	cmd.SetArgs([]string{"--operation", id, "--revision", "1", "--digest", digest})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
}
