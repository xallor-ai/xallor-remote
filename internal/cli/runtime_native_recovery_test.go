package cli

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/xallor-ai/xallor-remote/internal/release"
	"github.com/xallor-ai/xallor-remote/internal/runtimeimage"
	"github.com/xallor-ai/xallor-remote/internal/runtimeowner"
)

func runNativeRecovery(t *testing.T, store release.Store) {
	t.Helper()
	before, err := store.Read()
	if err != nil {
		t.Fatal(err)
	}
	baseline := release.ObserveSelection(before)
	var rawManifest []byte
	if before.Current != nil {
		activation, err := store.Activation(before.CurrentActivation, *before.Current)
		if err != nil {
			t.Fatal(err)
		}
		rawManifest, err = os.ReadFile(filepath.Join(activation.Dir, "manifest.json"))
		if err != nil {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()
	command := cmdRuntime()
	var out, notice bytes.Buffer
	command.SetOut(&out)
	command.SetErr(&notice)
	command.SetArgs([]string{"rollback"})
	if err := command.ExecuteContext(ctx); err != nil {
		t.Fatal("explicit recovery", err, notice.String())
	}
	id := regexp.MustCompile(`[a-f0-9]{32}`).FindString(notice.String())
	if id == "" {
		t.Fatal("recovery did not expose operation ID")
	}
	record, settled, err := (runtimeowner.Journal{Dir: filepath.Join(store.Dir, "lifecycle")}).QuerySettled(id)
	if err != nil || !settled || record.Outcome != runtimeowner.Succeeded {
		t.Fatal(record, settled, err)
	}
	if record.Binding == nil || record.Binding.Baseline != baseline {
		t.Fatal("recovery lost original pending baseline", record)
	}
	binding := record.Binding
	if before.Current == nil {
		if !binding.ClearOnly || baseline.Pending.SHA256 == "" || baseline.Current.SHA256 != "" || baseline.Previous.SHA256 != "" || binding.Requested != (release.SelectionIdentity{}) || binding.Manifest != (release.Manifest{}) || binding.ManifestSHA256 != "" {
			t.Fatal("invalid clear-only recovery binding", binding)
		}
	} else {
		sum := sha256.Sum256(rawManifest)
		if binding.ClearOnly || binding.Manifest != *before.Current || binding.ManifestSHA256 != hex.EncodeToString(sum[:]) || binding.Requested.ActivationID != before.CurrentActivation || binding.Requested.SHA256 != before.Current.Artifact.SHA256 || binding.Requested.LaunchID != "" {
			t.Fatal("recovery lost original authenticated target", binding)
		}
	}
	if len(record.Effects) == 0 || record.Effects[len(record.Effects)-1].Purpose != "requested" {
		t.Fatal("recovery terminal did not fulfill requested target", record)
	}
}

// Even with a valid retained image/source, an ordinary launcher cannot leave
// recovery waiting forever on an open input pipe. No journal admission occurs.
func TestNativeRecoveryRoleBoundsOpenInheritedInput(t *testing.T) {
	_, binary, manifest := nativeOwnerBundle(t)
	s, err := runtimeStore()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("XR_TEST_FAIL_CANDIDATE_SHA", manifest.Artifact.SHA256)
	if err := s.InitializeOwner(filepath.Dir(binary), runtimeControl{}); err == nil {
		t.Fatal("expected failed bootstrap")
	}
	state, err := s.Read()
	if err != nil || state.Pending == nil {
		t.Fatal(state, err)
	}
	activation, err := s.Activation(state.PendingActivation, *state.Pending)
	if err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(filepath.Join(s.Dir, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, activation.Binary, guardedArgs("runtime-recovery")...)
	configureDetached(cmd)
	image, err := runtimeimage.Prepare(cmd, manifest.Artifact.SHA256)
	if err != nil {
		t.Fatal(err)
	}
	defer image.Close()
	input, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	output, err := cmd.CombinedOutput()
	if err == nil || ctx.Err() != nil || !strings.Contains(string(output), "recovery input not completed: context deadline exceeded") {
		t.Fatal("recovery did not bound input after source admission", err, ctx.Err(), string(output))
	}
	after, err := os.ReadFile(filepath.Join(s.Dir, "state.json"))
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("timed-out input changed selection", err)
	}
	if _, err := os.Stat(filepath.Join(s.Dir, "lifecycle", "operations.db")); !os.IsNotExist(err) {
		t.Fatal("timed-out input created an operation", err)
	}
}

// A stopped legacy pending upgrade is recovered by a separately authenticated
// owner role; the original activation and device identity remain authoritative.
func TestNativeRecoveryRoleRestoresInterruptedUpgrade(t *testing.T) {
	profile, binary, first := nativeOwnerBundle(t)
	s, err := runtimeStore()
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := stopNativeFixtureRuntime(); err != nil {
			t.Error(err)
		}
	}()
	if err := s.InitializeOwner(filepath.Dir(binary), runtimeControl{}); err != nil {
		t.Fatal(err)
	}
	identity := nativeIdentityDigest(t, profile)
	if err := (runtimeControl{}).Stop(); err != nil {
		t.Fatal(err)
	}
	state, err := s.Read()
	if err != nil {
		t.Fatal(err)
	}
	original := state.CurrentActivation
	bundle, next := nativeCandidate(t, binary, first, "pending-recovery")
	raw, err := os.ReadFile(filepath.Join(bundle, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	activation, err := s.StageActivation(context.Background(), filepath.Join(bundle, next.Artifact.Filename), next, raw)
	if err != nil {
		t.Fatal(err)
	}
	state.Pending = &next
	state.PendingActivation = activation.ID
	encoded, _ := json.Marshal(state)
	if err := os.WriteFile(filepath.Join(s.Dir, "state.json"), encoded, 0600); err != nil {
		t.Fatal(err)
	}
	runNativeRecovery(t, s)
	after, err := s.Read()
	if err != nil || after.Pending != nil || after.CurrentActivation != original {
		t.Fatal(after, err)
	}
	live, err := (runtimeControl{}).Running()
	if err != nil || live == nil || live.SHA256 != first.Artifact.SHA256 {
		t.Fatal(live, err)
	}
	if nativeIdentityDigest(t, profile) != identity {
		t.Fatal("recovery changed device identity")
	}
}

// When the first bootstrap never admitted a service, the retained authenticated
// pending image may clear only that failed selection without creating identity.
func TestNativeRecoveryRoleClearsFailedInitialBootstrap(t *testing.T) {
	profile, binary, manifest := nativeOwnerBundle(t)
	s, err := runtimeStore()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("XR_TEST_FAIL_CANDIDATE_SHA", manifest.Artifact.SHA256)
	if err := s.InitializeOwner(filepath.Dir(binary), runtimeControl{}); err == nil {
		t.Fatal("injected startup failure absent")
	}
	before, err := s.Read()
	if err != nil || before.Pending == nil || before.Current != nil {
		t.Fatal(before, err)
	}
	defer func() {
		if err := stopNativeFixtureRuntime(); err != nil {
			t.Error(err)
		}
	}()
	runNativeRecovery(t, s)
	after, err := s.Read()
	if err != nil || after.Pending != nil || after.Current != nil {
		t.Fatal(after, err)
	}
	live, err := (runtimeControl{}).Running()
	if err != nil || live != nil {
		t.Fatal("clear-only recovery started a service", live, err)
	}
	if _, err := os.Stat(filepath.Join(profile, "identity.db")); !os.IsNotExist(err) {
		t.Fatal("clear-only recovery initialized device identity", err)
	}
}
