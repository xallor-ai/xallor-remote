package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/xallor-ai/xallor-remote/internal/buildinfo"
	"github.com/xallor-ai/xallor-remote/internal/release"
	"github.com/xallor-ai/xallor-remote/internal/runtimeimage"
	"github.com/xallor-ai/xallor-remote/internal/runtimeowner"
	"github.com/xallor-ai/xallor-remote/internal/runtimeprocess"
)

// Test-only service role pauses after actual source/permit authentication and
// before acquiring runtime.lock. Production exposes no such pause mechanism.
func runDelayedAuthorizedServiceFixture() int {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	raw, err := runtimeowner.ReadHandoffPipe(ctx, os.Stdin)
	if err != nil {
		return 81
	}
	var permit release.LaunchPermit
	if json.Unmarshal(raw, &permit) != nil {
		return 82
	}
	s, err := runtimeStore()
	if err != nil {
		return 83
	}
	live := buildinfo.Running()
	parent, err := runtimeprocess.WatchExit(os.Getppid())
	if err != nil {
		return 84
	}
	defer parent.Close()
	if err := s.AuthorizeLaunch(ctx, permit, live, os.Getppid()); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 85
	}
	profile := filepath.Dir(s.Dir)
	if err := os.WriteFile(filepath.Join(profile, "delayed-authorized"), nil, 0600); err != nil {
		return 86
	}
	for {
		if _, err := os.Stat(filepath.Join(profile, "delayed-release")); err == nil {
			break
		}
		select {
		case <-ctx.Done():
			return 87
		case <-time.After(10 * time.Millisecond):
		}
	}
	gate := &launchCommitGate{selected: func() (bool, error) { return s.LaunchSelected(permit, live) }, exited: parent.Exited, closeObserver: parent.Close, deadline: time.Now().Add(time.Minute)}
	defer gate.Close()
	if err := runRuntimeService(gate.Check, permit.LaunchID, permit.ActivationID); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 88
	}
	return 0
}

// A real child has already passed source authorization when recovery clears its
// pending state. Releasing that child afterward must not create identity or IPC;
// its original parent intentionally remains alive, isolating permit revocation.
func TestNativeRecoveryPreventsDelayedAuthorizedChildFromInitializing(t *testing.T) {
	profile, binary, manifest := nativeOwnerBundle(t)
	s, err := runtimeStore()
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(filepath.Dir(binary), "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	a, err := s.StageActivation(context.Background(), binary, manifest, raw)
	if err != nil {
		t.Fatal(err)
	}
	state := release.State{Pending: &manifest, PendingActivation: a.ID, PendingLaunchID: strings.Repeat("a", 32)}
	encoded, _ := json.Marshal(state)
	if err := os.WriteFile(filepath.Join(s.Dir, "state.json"), encoded, 0600); err != nil {
		t.Fatal(err)
	}
	permit, err := s.NewLaunchPermit(a.Binary, manifest.Artifact.SHA256, os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	payload, _ := json.Marshal(permit)
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, a.Binary, "--xr-delayed-authorized-service")
	configureDetached(cmd)
	cmd.Stdin = bytes.NewReader(payload)
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	image, err := runtimeimage.Prepare(cmd, manifest.Artifact.SHA256)
	if err != nil {
		t.Fatal(err)
	}
	defer image.Close()
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	joined := false
	defer func() {
		os.WriteFile(filepath.Join(profile, "delayed-release"), nil, 0600)
		if !joined {
			select {
			case <-done:
			case <-time.After(10 * time.Second):
				cmd.Process.Kill()
				<-done
				t.Error("delayed child required forced cleanup")
			}
		}
	}()
	deadline := time.Now().Add(time.Minute)
	for {
		if _, err := os.Stat(filepath.Join(profile, "delayed-authorized")); err == nil {
			break
		}
		select {
		case err := <-done:
			joined = true
			t.Fatal("child did not reach authorized barrier", err, output.String())
		default:
		}
		if time.Now().After(deadline) {
			t.Fatal("authorization barrier timed out")
		}
		time.Sleep(20 * time.Millisecond)
	}
	runNativeRecovery(t, s)
	if err := os.WriteFile(filepath.Join(profile, "delayed-release"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		joined = true
		if err == nil || !strings.Contains(output.String(), "not selected") {
			t.Fatal("old child was not rejected", err, output.String())
		}
	case <-time.After(10 * time.Second):
		t.Fatal("old child did not exit after revocation")
	}
	for _, name := range []string{"identity.db", "workspace", "ipc.sock"} {
		if _, err := os.Stat(filepath.Join(profile, name)); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("revoked child initialized state", name, err)
		}
	}
}
