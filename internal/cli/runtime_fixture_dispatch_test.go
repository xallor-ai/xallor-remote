package cli

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/xallor-ai/xallor-remote/internal/process"
)

// Only the bounded dispatch-regression subprocess reaches this sentinel.
// Its marker proves m.Run was entered, without starting any lifecycle tests.
func TestNativeFixtureDispatchSentinel(t *testing.T) {
	marker := os.Getenv("XR_TEST_DISPATCH_MARKER")
	if marker == "" {
		t.Skip("dispatch-regression subprocess only")
	}
	if err := os.WriteFile(marker, []byte("test-suite-entered"), 0600); err != nil {
		t.Fatal(err)
	}
}

// Losing a fixture mode must fail closed, not execute the complete test suite
// from a copied executable receiving a production-shaped version command.
func TestNativeFixtureDispatch_shouldRejectUnconfiguredVersion_withoutRunningSuite(t *testing.T) {
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(t.TempDir(), "suite-entered")
	cmd := exec.Command(self, "version", "--json")
	for _, value := range os.Environ() {
		key, _, _ := strings.Cut(value, "=")
		if !strings.HasPrefix(strings.ToUpper(key), "XR_TEST_") {
			cmd.Env = append(cmd.Env, value)
		}
	}
	cmd.Env = append(cmd.Env, "XR_TEST_DISPATCH_MARKER="+marker)
	var out, diagnostic bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &diagnostic
	process.HideConsole(cmd)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	child, err := process.Start(ctx, cmd)
	if err != nil {
		t.Fatal(err)
	}
	err = child.Wait()
	if errors.Is(err, process.ErrCleanup) {
		t.Fatal("dispatch probe cleanup unconfirmed", err)
	}
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 24 {
		t.Errorf("unconfigured fixture did not fail closed: %v", err)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Error("version command entered test suite")
	}
}
