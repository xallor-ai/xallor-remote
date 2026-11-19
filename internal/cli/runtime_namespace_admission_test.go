package cli

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/xallor-ai/xallor-remote/internal/ipc"
	"github.com/xallor-ai/xallor-remote/internal/protocol"
	"github.com/xallor-ai/xallor-remote/internal/runtimeowner"
)

// An actual authenticated service refuses a correctly shaped request aimed at
// another profile before creating a worker journal or disturbing its identity.
func TestNativeOwner_should_refuseForeignNamespace_beforeLifecycleAdmission(t *testing.T) {
	profile, binary, _ := nativeOwnerBundle(t)
	s, err := runtimeStore()
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := stopNativeFixtureRuntime(); err != nil {
			t.Error(err)
		}
	}()
	if err := s.Install(filepath.Dir(binary), runtimeControl{}); err != nil {
		t.Fatal(err)
	}
	before, err := s.Read()
	if err != nil {
		t.Fatal(err)
	}
	live, err := (runtimeControl{}).Running()
	if err != nil || live == nil {
		t.Fatal(live, err)
	}
	identity := nativeIdentityDigest(t, profile)
	foreignDir := filepath.Join(t.TempDir(), "runtime", "lifecycle")
	foreign, err := (runtimeowner.Journal{Dir: foreignDir}).NamespaceID()
	if err != nil {
		t.Fatal(err)
	}
	command, err := runtimeowner.NewCommand("rollback", "")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := ipc.Dial(time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()
	_ = raw.SetDeadline(time.Now().Add(10 * time.Second))
	conn := ipc.NewConn(raw)
	if err := ipc.Call(conn, "foreign-operation", "runtime.rollback", map[string]string{
		"operation_id": command.ID, "namespace_sha256": foreign}); err != nil {
		t.Fatal(err)
	}
	frame, err := conn.ReadFrame()
	if err != nil || frame.OK == nil || *frame.OK || frame.Code != protocol.RuntimeProfileMismatch {
		t.Fatal(frame, err)
	}
	for _, path := range []string{filepath.Join(s.Dir, "lifecycle", "operations.db"), filepath.Join(s.Dir, "transaction.log"), foreignDir} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatal("wrong-profile request created lifecycle state", path, err)
		}
	}
	after, err := s.Read()
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("wrong-profile request changed selection", err)
	}
	still, err := (runtimeControl{}).Running()
	if err != nil || still == nil || still.PID != live.PID || nativeIdentityDigest(t, profile) != identity {
		t.Fatal("wrong-profile request disturbed the serving identity", still, err)
	}
}
