package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/xallor-ai/xallor-remote/internal/buildinfo"
	"github.com/xallor-ai/xallor-remote/internal/release"
)

// Only the native _test executable has this mode. The production source
// verifier is unchanged; its independently trusted gh is a controlled process
// fixture. No cryptographic/source-authenticity claim follows from this test.
func runNativeOwnerFixture() int {
	if len(os.Args) == 2 && os.Args[1] == "--xr-independent-handoff-leaf" {
		return runIndependentHandoffLeaf()
	}
	if len(os.Args) > 1 && os.Args[1] == "--xr-independent-handoff-agent" {
		return runIndependentHandoffAgent(os.Args[2:])
	}
	if len(os.Args) == 2 && (os.Args[1] == "--xr-independent-exec-root" || os.Args[1] == "--xr-independent-exec-leaf") {
		return runIndependentExecFixture(os.Args[1])
	}
	if len(os.Args) >= 4 && os.Args[1] == "attestation" && os.Args[2] == "verify" {
		required := map[string]string{"--hostname": "github.com", "--repo": "xallor-ai/xallor-remote",
			"--signer-workflow":  "xallor-ai/xallor-remote/.github/workflows/release.yml",
			"--cert-oidc-issuer": "https://token.actions.githubusercontent.com", "--predicate-type": "https://slsa.dev/provenance/v1",
			"--source-ref": "refs/tags/v" + buildinfo.Version(), "--source-digest": strings.Repeat("a", 40), "--format": "json"}
		for flag, value := range required {
			found := false
			for i := 4; i+1 < len(os.Args); i++ {
				if os.Args[i] == flag && os.Args[i+1] == value {
					found = true
					break
				}
			}
			if !found {
				return 70
			}
		}
		deniesSelfHosted := false
		for _, arg := range os.Args[4:] {
			if arg == "--deny-self-hosted-runners" {
				deniesSelfHosted = true
			}
		}
		if !deniesSelfHosted {
			return 70
		}
		sum, err := buildinfo.HashFile(os.Args[3])
		if err != nil {
			return 71
		}
		_ = json.NewEncoder(os.Stdout).Encode([]any{map[string]any{"verificationResult": map[string]any{"statement": map[string]any{
			"subject": []any{map[string]any{"digest": map[string]string{"sha256": sum}}},
		}}}})
		return 0
	}
	if marker := os.Getenv("XR_TEST_PROBE_INTENT_MARKER"); marker != "" && len(os.Args) == 3 && os.Args[1] == "version" {
		if err := os.WriteFile(marker, []byte("executed"), 0600); err != nil {
			return 74
		}
	}
	buildinfo.Revision = strings.Repeat("a", 40)
	buildinfo.Modified = "false"
	// This marker is emitted only after this exact head completed the real
	// ensure flow, including its final owner handshake. Another head's readiness
	// must not let the test stop the owner before this one finishes startup.
	if marker := os.Getenv("XR_TEST_NATIVE_START_READY"); marker != "" && len(os.Args) == 2 && os.Args[1] == "start" {
		command := cmdStartWith(func(c release.Control) error {
			if err := ensureRuntimeWithControl(c); err != nil {
				return err
			}
			raw, _ := json.Marshal(map[string]int{"head_pid": os.Getpid()})
			if err := os.WriteFile(marker+".tmp", raw, 0600); err != nil {
				return err
			}
			return os.Rename(marker+".tmp", marker)
		})
		command.SetArgs([]string{})
		if err := command.Execute(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 72
		}
		return 0
	}
	if len(os.Args) == 2 && os.Args[1] == "--xr-delayed-authorized-service" {
		return runDelayedAuthorizedServiceFixture()
	}
	service := false
	for _, arg := range os.Args[1:] {
		if arg == "runtime-serve" {
			service = true
		}
	}
	if service {
		nativeLifecycleEvent("service-enter")
		defer nativeLifecycleEvent("service-return")
	}
	if len(os.Args) == 4 && os.Args[1] == "--xr-launch-without-commit" {
		if err := (runtimeControl{}).Start(os.Args[2], os.Args[3]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 74
		}
		_, _ = io.Copy(io.Discard, os.Stdin)
		return 0
	}
	for _, arg := range os.Args[1:] {
		// The owner, not the invoking head, now starts candidates. Bind fault
		// injection to this isolated test profile instead of the head's env.
		failSHA, _ := os.ReadFile(filepath.Join(os.Getenv("XALLOR_REMOTE_DATA_DIR"), "native-fail-candidate-sha"))
		if arg == "runtime-serve" && (os.Getenv("XR_TEST_FAIL_CANDIDATE_SHA") == buildinfo.Running().SHA256 || strings.TrimSpace(string(failSHA)) == buildinfo.Running().SHA256) {
			return 73
		}
	}
	if err := Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 72
	}
	return 0
}

// Test-only process evidence excludes credentials, command payloads and output.
// An entered service with no return event may have been terminated externally;
// this is diagnostic data, not proof of why a process exited.
func nativeLifecycleEvent(event string) {
	profile := os.Getenv("XALLOR_REMOTE_DATA_DIR")
	if profile == "" {
		return
	}
	file, err := os.OpenFile(filepath.Join(profile, "native-process-events.jsonl"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return
	}
	defer file.Close()
	_ = json.NewEncoder(file).Encode(map[string]any{"event": event, "pid": os.Getpid(), "parent_pid": os.Getppid(), "at": time.Now().UTC().Format(time.RFC3339Nano)})
}

// The fixture's head is reaped before failure cleanup. Retry only idempotent
// stop/observation in its isolated profile, never business requests or starts.
// Windows foreground tests additionally own a Job Object; Linux container runs
// retain the enclosing PID namespace as a cleanup boundary for setsid children.
func stopNativeFixtureRuntime() error {
	deadline := time.Now().Add(20 * time.Second)
	clear := 0
	var last error
	for time.Now().Before(deadline) {
		last = (runtimeControl{}).Stop()
		if last == nil {
			live, err := (runtimeControl{}).Running()
			last = err
			if err == nil && live == nil {
				clear++
				if clear == 3 {
					return nil
				}
			} else {
				clear = 0
			}
		} else {
			clear = 0
		}
		time.Sleep(100 * time.Millisecond)
	}
	return errors.Join(errors.New("isolated Runtime stop not confirmed"), last)
}

func nativeOwnerBundle(t *testing.T) (string, string, release.Manifest) {
	t.Helper()
	profile := filepath.Join(t.TempDir(), "profile")
	t.Setenv("XALLOR_REMOTE_DATA_DIR", profile)
	t.Setenv("XALLOR_REMOTE_RELAY_URL", "ws://127.0.0.1:1/remote")
	t.Setenv("XR_TEST_NATIVE_OWNER_FLOW", "1")
	source, tools := t.TempDir(), t.TempDir()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	image, err := os.ReadFile(self)
	if err != nil {
		t.Fatal(err)
	}
	name, gh := "xallor-remote", "gh"
	if runtime.GOOS == "windows" {
		name += ".exe"
		gh += ".exe"
	}
	binary := filepath.Join(source, name)
	for _, file := range []string{binary, filepath.Join(tools, gh)} {
		if err := os.WriteFile(file, image, 0700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", tools)
	sha, err := buildinfo.HashFile(binary)
	if err != nil {
		t.Fatal(err)
	}
	// Preserve the explicit owner-fixture dispatch, which implements version
	// with the mechanical build identity. Reuse production process supervision,
	// bounded output and the existing probe deadline, rather than bare Output.
	live, err := probeRuntimeIdentity(context.Background(), binary, sha)
	if err != nil {
		t.Fatal("native fixture version failed", err)
	}
	if live.Dirty || live.Revision != strings.Repeat("a", 40) {
		t.Fatal("invalid mechanical fixture identity", live.Info)
	}
	m := release.Manifest{Schema: 1, Build: live.Info, Artifact: release.Artifact{Filename: name, Size: int64(len(image)), SHA256: live.SHA256}}
	raw, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "manifest.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	return profile, binary, m
}
