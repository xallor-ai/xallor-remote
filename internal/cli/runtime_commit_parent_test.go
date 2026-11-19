package cli

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/xallor-ai/xallor-remote/internal/release"
	"github.com/xallor-ai/xallor-remote/internal/runtimeprocess"
)

// The real child finishes source admission, but its parent never commits
// current. Parent exit must stop that pending child without clearing recovery.
// Only gh is controlled test wiring; this is not signed release evidence.
func TestNativeLaunch_should_exitWhenParentDiesBeforeCommit(t *testing.T) {
	profile, binary, m := nativeOwnerBundle(t)
	s, err := runtimeStore()
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(filepath.Dir(binary), "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	a, err := s.StageActivation(context.Background(), binary, m, raw)
	if err != nil {
		t.Fatal(err)
	}
	state, _ := json.Marshal(release.State{Pending: &m, PendingActivation: a.ID})
	stateFile := filepath.Join(profile, "runtime/state.json")
	if err := os.WriteFile(stateFile, state, 0600); err != nil {
		t.Fatal(err)
	}
	parent := exec.Command(binary, "--xr-launch-without-commit", a.Binary, m.Artifact.SHA256)
	configureDetached(parent)
	pipe, err := parent.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	log, err := os.Create(filepath.Join(t.TempDir(), "parent.log"))
	if err != nil {
		t.Fatal(err)
	}
	defer log.Close()
	parent.Stdout, parent.Stderr = log, log
	if err := parent.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- parent.Wait() }()
	reaped := false
	defer func() {
		pipe.Close()
		if !reaped {
			_ = parent.Process.Kill()
			<-done
		}
		if err := stopNativeFixtureRuntime(); err != nil {
			t.Error(err)
		}
	}()
	var child runtimeprocess.ExitWatch
	deadline := time.Now().Add(90 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case err := <-done:
			reaped = true
			t.Fatal("launch parent exited before IPC", err)
		default:
		}
		live, err := (runtimeControl{}).Running()
		if err == nil && live != nil {
			child, err = runtimeprocess.WatchExit(live.PID)
			if err != nil {
				t.Fatal(err)
			}
			break
		}
		time.Sleep(30 * time.Millisecond)
	}
	if child == nil {
		t.Fatal("pending child never opened diagnostic IPC")
	}
	defer child.Close()
	pipe.Close()
	select {
	case err := <-done:
		reaped = true
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("parent did not exit")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := child.Wait(ctx); err != nil {
		t.Fatal("pending child survived departed parent", err)
	}
	after, err := os.ReadFile(stateFile)
	if err != nil || string(after) != string(state) {
		t.Fatal("parent loss changed pending recovery", err)
	}
}
