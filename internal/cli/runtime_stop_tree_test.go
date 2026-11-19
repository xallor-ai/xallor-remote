package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/xallor-ai/xallor-remote/internal/access"
	"github.com/xallor-ai/xallor-remote/internal/admission"
	"github.com/xallor-ai/xallor-remote/internal/delegation"
	"github.com/xallor-ai/xallor-remote/internal/execution"
	"github.com/xallor-ai/xallor-remote/internal/identity"
	"github.com/xallor-ai/xallor-remote/internal/job"
	"github.com/xallor-ai/xallor-remote/internal/principal"
	"github.com/xallor-ai/xallor-remote/internal/protocol"
	"github.com/xallor-ai/xallor-remote/internal/relay"
	"github.com/xallor-ai/xallor-remote/internal/runtimeprocess"
	"github.com/xallor-ai/xallor-remote/internal/storage"
)

// The fixture writes only inside its test workspace and self-expires as a
// cleanup backstop. Its normal child retains the parent's supervision boundary.
func runStopTreeFixture(role string) int {
	dir := os.Getenv("XR_TEST_STOP_TREE_DIR")
	if role == "--xr-tree-child" {
		deadline := time.Now().Add(30 * time.Second)
		for time.Now().Before(deadline) {
			if err := os.WriteFile(filepath.Join(dir, "heartbeat"), []byte(time.Now().String()), 0600); err != nil {
				return 51
			}
			time.Sleep(40 * time.Millisecond)
		}
		return 0
	}
	self, err := os.Executable()
	if err != nil {
		return 52
	}
	child := exec.Command(self, "--xr-tree-child")
	quietTreeChild(child)
	if err := child.Start(); err != nil {
		return 53
	}
	defer func() {
		if child.ProcessState == nil {
			_ = child.Process.Kill()
			_ = child.Wait()
		}
	}()
	raw, _ := json.Marshal(map[string]int{"parent": os.Getpid(), "child": child.Process.Pid})
	if err := os.WriteFile(filepath.Join(dir, "tree-pids.json"), raw, 0600); err != nil {
		return 54
	}
	// Start confirms process creation, not completion of Windows console/runtime
	// initialization. This fixture tests stopping an established tree; the
	// process package separately tests children born during termination.
	readyDeadline := time.Now().Add(10 * time.Second)
	for {
		if _, err := os.Stat(filepath.Join(dir, "heartbeat")); err == nil {
			break
		}
		if time.Now().After(readyDeadline) {
			return 56
		}
		time.Sleep(time.Millisecond)
	}
	fmt.Println("tree-started")
	if err := child.Wait(); err != nil {
		return 55
	}
	return 0
}

// Use real Relay authentication/leases and the production daemon/executor.
// Stop's success must precede neither descendant exit nor durable job release.
func TestStop_should_reclaimAuthenticatedTaskTree_beforeReportingSuccess(t *testing.T) {
	profile := t.TempDir()
	t.Setenv("XALLOR_REMOTE_DATA_DIR", profile)
	workspace := filepath.Join(profile, "workspace")
	if err := os.Mkdir(workspace, 0700); err != nil {
		t.Fatal(err)
	}
	db, err := storage.OpenSQLStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	key, err := db.ExecutionKey()
	if err != nil {
		t.Fatal(err)
	}
	accounts, permissions := principal.NewService(db), access.NewService(db)
	hub := relay.NewHub(db, accounts, permissions, delegation.NewAuthority(key, db, accounts, permissions), admission.New(admission.Limits{PerDevice: protocol.MaxDeviceRequests}))
	server := httptest.NewServer(relay.Handler(hub, nil))
	defer server.Close()
	url := "ws" + strings.TrimPrefix(server.URL, "http")
	t.Setenv("XALLOR_REMOTE_RELAY_URL", url)
	store, err := identity.LoadFrom(profile)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetConfig(url, workspace, "auto"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.SetInbound(true); err != nil {
		t.Fatal(err)
	}
	id, _, grant, _, _, _, _ := store.Snapshot()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, self)
	configureDetached(cmd)
	cmd.Env = append(os.Environ(), "XR_TEST_ACTUAL_RUNTIME=1", "XR_TEST_STOP_TREE_DIR="+workspace)
	diagnostic, err := os.Create(filepath.Join(t.TempDir(), "runtime-diagnostic.log"))
	if err != nil {
		t.Fatal(err)
	}
	defer diagnostic.Close()
	cmd.Stderr = diagnostic
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	var watches []runtimeprocess.ExitWatch
	defer func() {
		_ = (runtimeControl{}).Stop()
		// No PID-only cleanup of descendants: held observers wait for the
		// controlled fixture's finite self-expiry if supervision failed.
		cleanup, done := context.WithTimeout(context.Background(), 35*time.Second)
		defer done()
		for _, watch := range watches {
			_ = watch.Wait(cleanup)
			_ = watch.Close()
		}
		if cmd.ProcessState == nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
	}()
	for {
		status, err := rpc("status", map[string]any{})
		if err == nil && status["online"] == true {
			break
		}
		if ctx.Err() != nil {
			t.Fatal("isolated Runtime did not connect", err)
		}
		time.Sleep(20 * time.Millisecond)
	}
	client, _, err := websocket.Dial(ctx, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer client.CloseNow()
	if err := wsjson.Write(ctx, client, protocol.Message{Type: protocol.TypeHelloClient, TargetID: id, Grant: grant}); err != nil {
		t.Fatal(err)
	}
	var ack protocol.Message
	if err := wsjson.Read(ctx, client, &ack); err != nil || !ack.ConfirmsClientAuth(false) {
		t.Fatal("explicit client authentication missing", err)
	}
	payload, err := json.Marshal(execution.Request{Program: &execution.Program{Executable: self, Args: []string{"--xr-tree-parent"}}, TimeoutMS: 30000})
	if err != nil {
		t.Fatal(err)
	}
	if err := wsjson.Write(ctx, client, protocol.Message{Type: protocol.TypeInvoke, ExecID: "stop-tree", Op: protocol.OpExec, Payload: payload}); err != nil {
		t.Fatal(err)
	}
	for {
		var msg protocol.Message
		if err := wsjson.Read(ctx, client, &msg); err != nil {
			t.Fatal(err)
		}
		if msg.Type == protocol.TypeStdout && strings.Contains(msg.Data, "tree-started") {
			break
		}
		if msg.Type == protocol.TypeExit || msg.Type == protocol.TypeError || msg.Type == protocol.TypeInvokeNack {
			t.Fatalf("tree did not start: type=%s code=%s", msg.Type, msg.Code)
		}
	}
	var pids struct{ Parent, Child int }
	raw, err := os.ReadFile(filepath.Join(workspace, "tree-pids.json"))
	if err != nil || json.Unmarshal(raw, &pids) != nil {
		t.Fatal("missing owned tree identity", err)
	}
	for _, pid := range []int{pids.Parent, pids.Child} {
		watch, err := runtimeprocess.WatchExit(pid)
		if err != nil {
			t.Fatal(err)
		}
		watches = append(watches, watch)
		if exited, err := watch.Exited(); err != nil || exited {
			t.Fatal("fixture was not alive before stop", exited, err)
		}
	}
	var disk struct {
		Version int                   `json:"version"`
		Jobs    map[string]job.Record `json:"jobs"`
	}
	raw, err = os.ReadFile(filepath.Join(profile, "jobs.json"))
	if err != nil || json.Unmarshal(raw, &disk) != nil || len(disk.Jobs) != 1 {
		t.Fatal("task occupancy missing before stop", err)
	}
	for _, record := range disk.Jobs {
		if record.State != "occupied" {
			t.Fatal("child started without occupied state")
		}
	}
	command := cmdStop()
	var printed bytes.Buffer
	command.SetOut(&printed)
	if err := command.RunE(command, nil); err != nil {
		if raw, readErr := os.ReadFile(diagnostic.Name()); readErr == nil {
			for _, line := range strings.Split(string(raw), "\n") {
				if strings.Contains(line, "task resource cleanup unconfirmed") {
					t.Log(line)
				}
			}
		}
		t.Fatal("tree shutdown unconfirmed", err)
	}
	for index, watch := range watches {
		if exited, err := watch.Exited(); err != nil || !exited {
			t.Fatalf("descendant still live after success: tree_index=%d parent_pid=%d child_pid=%d exited=%t error=%v", index, pids.Parent, pids.Child, exited, err)
		}
	}
	if err := cmd.Wait(); err != nil {
		t.Fatal(err)
	}
	raw, err = os.ReadFile(filepath.Join(profile, "jobs.json"))
	disk.Jobs = nil // Decode a new observation; JSON does not delete existing map keys.
	if err != nil || json.Unmarshal(raw, &disk) != nil || len(disk.Jobs) != 0 {
		t.Fatal("task occupancy not released", err)
	}
	if printed.String() != "已停止。\n" {
		t.Fatal("missing confirmed stop output")
	}
}
