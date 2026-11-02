package handoff

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// A healthy cold CLI taking over eight seconds must pass; caller cancellation still wins.
func TestPreflight_should_allowColdCLI_and_honorCallerCancellation(t *testing.T) {
	if os.Getenv("XR_PREFLIGHT_COLD_HELPER") == "1" {
		if os.Args[len(os.Args)-1] == "--help" {
			time.Sleep(9 * time.Second)
			fmt.Println("--resume --workspace --mode --print --output-format stream-json")
		} else {
			fmt.Println("fixture-1")
		}
		os.Exit(0)
	}
	workspace := t.TempDir()
	git := func(args ...string) string {
		t.Helper()
		b, err := exec.Command("git", append([]string{"-C", workspace}, args...)...).CombinedOutput()
		if err != nil {
			t.Fatalf("git: %v %s", err, b)
		}
		return strings.TrimSpace(string(b))
	}
	git("init", "--quiet")
	git("-c", "user.name=Test", "-c", "user.email=test@example.invalid", "commit", "--allow-empty", "-m", "fixture", "--quiet")
	r := testRequest(workspace)
	r.Revision = git("rev-parse", "HEAD")
	for _, interrupt := range []bool{false, true} {
		ctx, cancel := context.WithCancel(context.Background())
		t.Setenv("XR_PREFLIGHT_COLD_HELPER", "1")
		m := New(context.Background(), t.TempDir())
		if interrupt {
			cancel()
		}
		if err := Configure(m.dir, Config{Executable: os.Args[0], Prefix: []string{"-test.run=^TestPreflight_should_allowColdCLI_and_honorCallerCancellation$", "--"}}); err != nil {
			t.Fatal(err)
		}
		_, version, _, err := m.preflight(ctx, r, workspace)
		cancel()
		m.Close()
		if interrupt {
			if err == nil || err.Error() != "preflight_interrupted" {
				t.Fatalf("caller cancellation lost: %v", err)
			}
		} else if err != nil || version != "fixture-1" {
			t.Fatalf("healthy cold CLI rejected: %q %v", version, err)
		}
	}
}

// Cancellation/deadline are operational uncertainty, not proof of an incompatible CLI/repository.
func TestPreflight_should_preserveInterruptionCause(t *testing.T) {
	for _, expired := range []bool{false, true} {
		ctx, cancel := context.WithCancel(context.Background())
		code := "preflight_interrupted"
		if expired {
			cancel()
			ctx, cancel = context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
			code = "preflight_timeout_git_root"
		}
		cancel()
		m := New(context.Background(), t.TempDir())
		workspace := t.TempDir()
		_, _, _, err := m.preflight(ctx, testRequest(workspace), workspace)
		m.Close()
		if err == nil || err.Error() != code {
			t.Fatalf("expected %s, got %v", code, err)
		}
	}
}
