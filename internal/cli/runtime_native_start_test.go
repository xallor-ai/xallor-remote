package cli

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/xallor-ai/xallor-remote/internal/process"
)

// Exercise public start -> source check -> private activation -> protected
// child -> inherited permit -> real service admission -> current -> ready.
// Only the trusted gh process is a proof fixture; the complete Go path runs.
func TestNativeStart_should_admitSelectedChild_andReuseOwner(t *testing.T) {
	t.Log("Scope: native production start/service flow with a controlled gh fixture; not cryptographic acceptance.")
	profile, binary, manifest := nativeOwnerBundle(t)
	log, err := os.Create(filepath.Join(t.TempDir(), "start.log"))
	if err != nil {
		t.Fatal(err)
	}
	defer log.Close()
	readyMarker := filepath.Join(profile, "foreground-head-ready.json")
	parent := exec.Command(binary, "start")
	parent.Env = append(os.Environ(), "XR_TEST_NATIVE_START_READY="+readyMarker)
	parent.Stdout, parent.Stderr = log, log
	supervised, err := process.Start(context.Background(), parent)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- supervised.Wait() }()
	exited := false
	defer func() {
		if !exited {
			if err := supervised.Stop(); err != nil {
				t.Error("native head cleanup failed", err)
			}
			if err := <-done; errors.Is(err, process.ErrCleanup) {
				t.Error("native head descendants unconfirmed", err)
			}
			exited = true
		}
		if err := stopNativeFixtureRuntime(); err != nil {
			t.Error("native Runtime cleanup unconfirmed", err)
		}
	}()
	deadline := time.Now().Add(150 * time.Second)
	var pid int
	for time.Now().Before(deadline) {
		select {
		case err := <-done:
			exited = true
			raw, _ := os.ReadFile(log.Name())
			t.Fatalf("public start exited before ready: %v %s", err, raw)
		default:
		}
		if handled, err := ownerEnsure(); handled && err == nil {
			live, err := (runtimeControl{}).Running()
			if err != nil || live == nil {
				t.Fatal("ready process missing", err)
			}
			pid = live.PID
			break
		}
		time.Sleep(30 * time.Millisecond)
	}
	if pid == 0 {
		raw, _ := os.ReadFile(log.Name())
		t.Fatalf("native owner did not become ready: %s", raw)
	}
	// The same owner's external readiness precedes completion of the initiating
	// head's source cleanup and final handshake. Wait for that head's own event
	// before stopping it, retaining the original overall startup deadline.
	for {
		raw, readErr := os.ReadFile(readyMarker)
		var ready struct {
			HeadPID int `json:"head_pid"`
		}
		if readErr == nil && json.Unmarshal(raw, &ready) == nil && ready.HeadPID == parent.Process.Pid {
			break
		}
		select {
		case err := <-done:
			exited = true
			output, _ := os.ReadFile(log.Name())
			t.Fatalf("foreground head exited before its own readiness: %v %s", err, output)
		default:
		}
		if time.Now().After(deadline) {
			t.Fatal("foreground head did not finish its own readiness handshake")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if pid == parent.Process.Pid {
		t.Fatal("head became the service process")
	}
	s, err := runtimeStore()
	if err != nil {
		t.Fatal(err)
	}
	state, err := s.Read()
	if err != nil || state.Current == nil || *state.Current != manifest || state.CurrentActivation == "" || state.Pending != nil {
		t.Fatal("selected state invalid", state, err)
	}
	if _, err := os.Stat(filepath.Join(profile, "identity.db")); err != nil {
		t.Fatal("admitted child has no identity", err)
	}
	reuseContext, reuseCancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer reuseCancel()
	repeated := exec.CommandContext(reuseContext, binary, "start")
	configureDetached(repeated)
	repeated.Stdout, repeated.Stderr = log, log
	if err := repeated.Run(); err != nil {
		t.Fatal("existing owner was not reused", err)
	}
	live, err := (runtimeControl{}).Running()
	if err != nil || live == nil || live.PID != pid {
		t.Fatal("repeat start replaced owner", live, err)
	}
	select {
	case err := <-done:
		exited = true
		t.Fatal("foreground head stopped waiting", err)
	default:
	}
	if err := (runtimeControl{}).Stop(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		exited = true
		if err != nil {
			raw, readErr := os.ReadFile(log.Name())
			t.Fatalf("foreground head did not finish cleanly: %v; log_read=%v; output=%s", err, readErr, raw)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("foreground head did not observe owner exit")
	}
}
