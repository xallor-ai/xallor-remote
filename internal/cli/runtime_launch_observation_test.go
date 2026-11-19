package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/xallor-ai/xallor-remote/internal/ipc"
	"github.com/xallor-ai/xallor-remote/internal/protocol"
	"github.com/xallor-ai/xallor-remote/internal/runtimeowner"
)

func nativeLaunchObservation(t *testing.T) (string, string, string) {
	t.Helper()
	raw, err := ipc.Dial(time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()
	_ = raw.SetDeadline(time.Now().Add(90 * time.Second))
	conn := ipc.NewConn(raw)
	if err := ipc.Call(conn, "launch-observation", "runtime.status", map[string]any{}); err != nil {
		t.Fatal(err)
	}
	frame, err := conn.ReadFrame()
	if err != nil || frame.OK == nil || !*frame.OK {
		t.Fatal(frame, err)
	}
	var response struct {
		Runtime struct {
			State, Code string
			LaunchID    string `json:"launch_id"`
		} `json:"runtime"`
	}
	if err := json.Unmarshal(frame.Result, &response); err != nil {
		t.Fatal(err)
	}
	return response.Runtime.State, response.Runtime.Code, response.Runtime.LaunchID
}

// The ready process must bind its authenticated launch permit, not echo a new
// launch ID read from selection state after startup. Same bytes are not the
// same launch. This is native mechanics with a controlled provenance verifier.
func TestNativeOwnerObservation_should_bindOriginalLaunch_whenSelectionNonceChanges(t *testing.T) {
	_, binary, _ := nativeOwnerBundle(t)
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
	selected, err := s.Read()
	if err != nil || len(selected.CurrentLaunchID) != 32 {
		t.Fatal(selected, err)
	}
	state, code, launch := nativeLaunchObservation(t)
	if state != "ready" || code != "" || launch != selected.CurrentLaunchID {
		t.Fatal("ready owner lost authenticated launch identity", state, code, launch)
	}
	path := filepath.Join(s.Dir, "state.json")
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := os.WriteFile(path, before, 0600); err != nil {
			t.Error(err)
		}
	}()
	selected.CurrentLaunchID = strings.Repeat("f", 32)
	if selected.CurrentLaunchID == launch {
		selected.CurrentLaunchID = strings.Repeat("e", 32)
	}
	changed, _ := json.Marshal(selected)
	if err := os.WriteFile(path, changed, 0600); err != nil {
		t.Fatal(err)
	}
	state, code, launch = nativeLaunchObservation(t)
	if state == "ready" || code != protocol.RuntimeStartFailed || launch != "" {
		t.Fatal("owner claimed another launch from mutable selection", state, code, launch)
	}
	if after, err := os.ReadFile(path); err != nil || !bytes.Equal(after, changed) {
		t.Fatal("observation repaired or changed selection state", err)
	}
	// Raw local callers cannot bypass the process binding by omitting ensure.
	command, err := runtimeowner.NewCommand("rollback", "")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := ipc.Dial(time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()
	_ = raw.SetDeadline(time.Now().Add(90 * time.Second))
	conn := ipc.NewConn(raw)
	namespace, err := (runtimeowner.Journal{Dir: filepath.Join(s.Dir, "lifecycle")}).NamespaceID()
	if err != nil {
		t.Fatal(err)
	}
	if err := ipc.Call(conn, "unbound-change", "runtime.rollback", map[string]string{"operation_id": command.ID, "namespace_sha256": namespace}); err != nil {
		t.Fatal(err)
	}
	frame, err := conn.ReadFrame()
	if err != nil || frame.OK == nil || *frame.OK {
		t.Fatal("unbound owner admitted lifecycle mutation", frame, err)
	}
	if _, err := os.Stat(filepath.Join(s.Dir, "lifecycle", "operations.db")); !os.IsNotExist(err) {
		t.Fatal("unbound request created a transaction journal", err)
	}
}
