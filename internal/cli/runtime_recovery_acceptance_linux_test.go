//go:build linux

package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/xallor-ai/xallor-remote/internal/release"
	"github.com/xallor-ai/xallor-remote/internal/runtimeimage"
	"github.com/xallor-ai/xallor-remote/internal/runtimeowner"
)

// A real recovery entry loses only its acceptance reader; its parent stays
// alive. The admitted clear-only rollback must settle despite acknowledgement
// EPIPE, without starting a service or leaving pending resource obligations.
func TestNativeRecoveryRole_shouldSettleClearOnlyRollback_whenAcceptancePipeIsClosed(t *testing.T) {
	profile, binary, manifest := nativeOwnerBundle(t)
	s, err := runtimeStore()
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(filepath.Dir(binary), "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	activation, err := s.StageActivation(ctx, binary, manifest, raw)
	if err != nil {
		t.Fatal(err)
	}
	state := release.State{Pending: &manifest, PendingActivation: activation.ID, PendingLaunchID: strings.Repeat("a", 32)}
	encoded, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(s.Dir, "state.json"), encoded, 0600); err != nil {
		t.Fatal(err)
	}
	command, err := runtimeowner.NewCommand("rollback", "")
	if err != nil {
		t.Fatal(err)
	}
	request, err := command.Request()
	if err != nil {
		t.Fatal(err)
	}
	permit, retained, err := s.NewRecoveryHandoff(ctx, os.Getpid(), request.ID, request.IntentSHA256)
	if err != nil {
		t.Fatal(err)
	}
	payload, err := runtimeowner.EncodeHandoff(runtimeowner.Handoff{Command: command, Permit: permit})
	if err != nil {
		t.Fatal(err)
	}
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	if err := reader.Close(); err != nil {
		t.Fatal(err)
	}
	cmd := exec.CommandContext(ctx, retained.Binary, guardedArgs("runtime-recovery")...)
	configureDetached(cmd)
	cmd.Stdin = bytes.NewReader(payload)
	cmd.Stdout = writer
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	image, err := runtimeimage.Prepare(cmd, manifest.Artifact.SHA256)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := image.Close(); err != nil {
			t.Error("recovery image close", err)
		}
	}()
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	closeErr := writer.Close()
	exitErr := cmd.Wait()
	var processExit *exec.ExitError
	// The fixture reports the delivery error as exit 72 after Worker.Run returns.
	// A signal exit, timeout or arbitrary nonzero status cannot satisfy this test.
	if closeErr != nil || ctx.Err() != nil || !errors.As(exitErr, &processExit) || processExit.ExitCode() != 72 || !strings.Contains(stderr.String(), "broken pipe") {
		t.Fatalf("recovery did not return acceptance delivery failure: exit=%v close=%v context=%v stderr=%s", exitErr, closeErr, ctx.Err(), stderr.String())
	}
	journal := runtimeowner.Journal{Dir: filepath.Join(s.Dir, "lifecycle")}
	record, settled, err := journal.QuerySettled(command.ID)
	if err != nil || !settled || record.Outcome != runtimeowner.Succeeded || record.ValidateFor(request) != nil {
		t.Fatalf("accepted recovery did not settle: outcome=%s settled=%v err=%v stderr=%s", record.Outcome, settled, err, stderr.String())
	}
	if record.Binding == nil || !record.Binding.ClearOnly || record.Binding.Baseline != release.ObserveSelection(state) {
		t.Fatal("recovery lost its original clear-only intent", record.Binding)
	}
	resources, err := journal.Resources(command.ID)
	if err != nil || len(resources) == 0 {
		t.Fatal("missing recovery resource receipts", resources, err)
	}
	for _, resource := range resources {
		if resource.Status != runtimeowner.ResourceClosed {
			t.Fatal("recovery retained resource debt", resource)
		}
	}
	if err := journal.RefuseBootstrap(); err != nil {
		t.Fatal("settled recovery still blocks bootstrap", err)
	}
	after, err := s.Read()
	if err != nil || after != (release.State{}) {
		t.Fatal("clear-only recovery did not clear pending state", after, err)
	}
	for _, name := range []string{"identity.db", "workspace", "ipc.sock"} {
		if _, err := os.Stat(filepath.Join(profile, name)); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("clear-only recovery initialized service state", name, err)
		}
	}
}
