package cli

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/xallor-ai/xallor-remote/internal/buildinfo"
	"github.com/xallor-ai/xallor-remote/internal/release"
	"github.com/xallor-ai/xallor-remote/internal/runtimeowner"
)

func nativeCandidate(t *testing.T, binary string, m release.Manifest, label string) (string, release.Manifest) {
	t.Helper()
	raw, err := os.ReadFile(binary)
	if err != nil {
		t.Fatal(err)
	}
	// An ignored executable overlay provides a distinct native image while the
	// private fixture still runs exactly the production CLI. Not a signed release.
	raw = append(raw, []byte("\nfixture-candidate-"+label)...)
	sum := sha256.Sum256(raw)
	m.Artifact.SHA256 = hex.EncodeToString(sum[:])
	m.Artifact.Size = int64(len(raw))
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, m.Artifact.Filename), raw, 0700); err != nil {
		t.Fatal(err)
	}
	metadata, _ := json.Marshal(m)
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), metadata, 0600); err != nil {
		t.Fatal(err)
	}
	return dir, m
}

func nativeIdentityDigest(t *testing.T, profile string) string {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(filepath.Join(profile, "identity.db"))+"?mode=ro")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var id, secret, grant string
	var inbound, active int
	if err := db.QueryRow("SELECT device_id,secret,grant,inbound,active FROM identity WHERE singleton=1").Scan(&id, &secret, &grant, &inbound, &active); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal([]any{id, secret, grant, inbound, active})
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// All lifecycle controls and processes are real; only the external trusted
// verifier is a native fixture. Distinct images exercise selected/previous
// snapshots without claiming two product releases or cryptographic acceptance.
func TestNativeLifecycle_should_upgradeRollbackAndRestore_withoutChangingIdentity(t *testing.T) {
	t.Log("Scope: native lifecycle with controlled gh and distinct image fixtures; not two signed product releases.")
	profile, binary, first := nativeOwnerBundle(t)
	secondBundle, second := nativeCandidate(t, binary, first, "second")
	failedBundle, failed := nativeCandidate(t, binary, first, "failed")
	workspace := filepath.Join(profile, "workspace")
	if err := os.MkdirAll(workspace, 0700); err != nil {
		t.Fatal(err)
	}
	config, _ := json.Marshal(map[string]string{"workspace": workspace, "relay_url": "ws://127.0.0.1:1/remote", "shell": "auto"})
	if err := os.WriteFile(filepath.Join(profile, "config.json"), config, 0600); err != nil {
		t.Fatal(err)
	}
	sentinel := filepath.Join(workspace, "user-file.txt")
	if err := os.WriteFile(sentinel, []byte("preserved fixture data"), 0600); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := stopNativeFixtureRuntime(); err != nil {
			t.Error("native fixture cleanup failed", err)
		}
	}()
	run := func(words ...string) (string, error) {
		t.Helper()
		// The production flow includes bounded source checks, a 75s child
		// admission window and owner readiness. Do not kill it at 35s and
		// mistake a fixture deadline for a product lifecycle failure.
		ctx, cancel := context.WithTimeout(context.Background(), 150*time.Second)
		defer cancel()
		started := time.Now()
		cmd := exec.CommandContext(ctx, binary, words...)
		configureDetached(cmd)
		output, err := cmd.CombinedOutput()
		if err != nil {
			if events, readErr := os.ReadFile(filepath.Join(profile, "native-process-events.jsonl")); readErr == nil {
				t.Logf("native process events: %s", events)
			}
			if raw, readErr := os.ReadFile(filepath.Join(profile, "runtime", "startup.log")); readErr == nil {
				for _, line := range strings.Split(string(raw), "\n") {
					if strings.Contains(line, "runtime startup selection invalid") {
						t.Log(line)
					}
				}
			}
		}
		t.Logf("native command %s completed in %s", words[0], time.Since(started).Round(time.Millisecond))
		if ctx.Err() != nil {
			return string(output), fmt.Errorf("native command deadline: %w", ctx.Err())
		}
		return string(output), err
	}
	var currentActivation string
	assertCurrent := func(expected release.Manifest) buildinfo.Executable {
		t.Helper()
		if handled, err := ownerEnsure(); !handled || err != nil {
			if events, readErr := os.ReadFile(filepath.Join(profile, "native-process-events.jsonl")); readErr == nil {
				t.Logf("native process events: %s", events)
			}
			if live, observeErr := (runtimeControl{}).Running(); observeErr == nil && live != nil {
				t.Logf("second read-only IPC observation: pid=%d sha=%s", live.PID, live.SHA256)
			} else {
				t.Logf("second read-only IPC observation: live=%t error=%v", live != nil, observeErr)
			}
			if lockErr := confirmRuntimeUnlocked(); lockErr != nil {
				t.Logf("runtime lifetime/occupancy observation: %v", lockErr)
			}
			// Print only gate diagnostics, never startup banners or credentials.
			if raw, readErr := os.ReadFile(filepath.Join(profile, "runtime", "startup.log")); readErr == nil {
				for _, line := range strings.Split(string(raw), "\n") {
					if strings.Contains(line, "runtime startup selection invalid") {
						t.Log(line)
					}
				}
			}
			t.Fatal("current owner not ready", handled, err)
		}
		s, err := runtimeStore()
		if err != nil {
			t.Fatal(err)
		}
		state, err := s.Read()
		if err != nil || state.Current == nil || *state.Current != expected || state.Pending != nil {
			t.Fatal("bad current selection", state, err)
		}
		currentActivation = state.CurrentActivation
		live, err := (runtimeControl{}).Running()
		if err != nil || live == nil || live.SHA256 != expected.Artifact.SHA256 {
			t.Fatal("wrong loaded native image", live, err)
		}
		return *live
	}
	if output, err := run("ensure"); err != nil {
		t.Fatal("cold native ensure", err, output)
	}
	original := assertCurrent(first)
	originalActivation := currentActivation
	identity := nativeIdentityDigest(t, profile)
	assertPreserved := func() {
		t.Helper()
		if nativeIdentityDigest(t, profile) != identity {
			t.Fatal("identity/grant changed during native lifecycle")
		}
		if got, err := os.ReadFile(filepath.Join(profile, "config.json")); err != nil || string(got) != string(config) {
			t.Fatal("config changed", err)
		}
		if got, err := os.ReadFile(sentinel); err != nil || string(got) != "preserved fixture data" {
			t.Fatal("user file changed", err)
		}
	}
	if output, err := run("runtime", "install", "--bundle", secondBundle); err != nil {
		t.Fatal("native upgrade", err, output)
	}
	upgraded := assertCurrent(second)
	assertPreserved()
	if upgraded.PID == original.PID {
		t.Fatal("upgrade reused old process")
	}
	if output, err := run("runtime", "rollback"); err != nil {
		t.Fatal("native rollback", err, output)
	}
	rolledBack := assertCurrent(first)
	assertPreserved()
	if currentActivation != originalActivation {
		t.Fatal("rollback replaced the original activation")
	}
	if rolledBack.PID == upgraded.PID {
		t.Fatal("rollback reused candidate process")
	}
	if err := os.WriteFile(filepath.Join(profile, "native-fail-candidate-sha"), []byte(failed.Artifact.SHA256), 0600); err != nil {
		t.Fatal(err)
	}
	output, err := run("runtime", "install", "--bundle", failedBundle)
	if err == nil {
		t.Fatal("candidate failure not restored", err, output)
	}
	operationID := regexp.MustCompile(`[a-f0-9]{32}`).FindString(output)
	store, storeErr := runtimeStore()
	if storeErr != nil {
		t.Fatal(storeErr)
	}
	receipt, settled, queryErr := (runtimeowner.Journal{Dir: filepath.Join(store.Dir, "lifecycle")}).QuerySettled(operationID)
	if queryErr != nil || receipt.Outcome != runtimeowner.Failed || !settled {
		t.Fatal("failure did not produce settled owner receipt", receipt, settled, queryErr, output)
	}
	failedStart := false
	for _, effect := range receipt.Effects {
		if effect.Name == "start" && effect.TargetSHA256 == failed.Artifact.SHA256 && effect.State == runtimeowner.EffectQuiescentFailure {
			failedStart = true
		}
	}
	if !failedStart {
		t.Fatal("failed candidate was not actually attempted", receipt)
	}
	restored := assertCurrent(first)
	assertPreserved()
	if currentActivation != originalActivation {
		t.Fatal("restore replaced the original activation")
	}
	if restored.PID == rolledBack.PID {
		t.Fatal("failed candidate did not exercise restart/restore")
	}
}
