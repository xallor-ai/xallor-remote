package cli

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/xallor-ai/xallor-remote/internal/buildinfo"
	"github.com/xallor-ai/xallor-remote/internal/release"
	"github.com/xallor-ai/xallor-remote/internal/runtimeprocess"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func runProbeResourceFixture(mode string) int {
	dir := os.Getenv("XR_PROBE_TREE_DIR")
	if mode == "noisy" {
		for i := 0; i < 256; i++ {
			os.Stdout.WriteString(strings.Repeat("x", 4096))
			os.Stderr.WriteString(strings.Repeat("y", 4096))
		}
		return 0
	}
	if mode == "tree-child" {
		time.Sleep(20 * time.Second)
		return 0
	}
	cmd := exec.Command(os.Getenv("XR_PROBE_ORIGINAL_BINARY"), "version", "--json")
	cmd.Env = append(os.Environ(), "XR_TEST_VERSION_PROBE=tree-child")
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	quietTreeChild(cmd)
	if err := cmd.Start(); err != nil {
		return 61
	}
	if err := os.WriteFile(filepath.Join(dir, "pid"), []byte(strconv.Itoa(cmd.Process.Pid)), 0600); err != nil {
		return 62
	}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(filepath.Join(dir, "release")); err == nil {
			json.NewEncoder(os.Stdout).Encode(buildinfo.Running())
			return 0
		}
		time.Sleep(10 * time.Millisecond)
	}
	return 63
}

// Probe closure includes a real descendant holding stdout open after root exit.
// A retained OS watch is acquired before releasing the parent, excluding PID reuse.
func TestRuntimeProbe_shouldCloseDescendantBeforeReceipt(t *testing.T) {
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	t.Setenv("XR_TEST_VERSION_PROBE", "tree")
	t.Setenv("XR_PROBE_TREE_DIR", dir)
	t.Setenv("XR_PROBE_ORIGINAL_BINARY", binary)
	p, _ := (runtimeControl{}).PrepareProbe(release.ProbeRequest{Binary: binary, Resource: release.ResourceObligation{ImageSHA256: buildinfo.Running().SHA256}})
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := p.Run(ctx); done <- err }()
	var pid int
	for pid == 0 {
		raw, _ := os.ReadFile(filepath.Join(dir, "pid"))
		pid, _ = strconv.Atoi(string(raw))
		if ctx.Err() != nil {
			t.Fatal(ctx.Err())
		}
		time.Sleep(10 * time.Millisecond)
	}
	watch, err := runtimeprocess.WatchExit(pid)
	if err != nil {
		t.Fatal(err)
	}
	defer watch.Close()
	if exited, err := watch.Exited(); err != nil || exited {
		t.Fatal("fixture descendant not alive", exited, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "release"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	receipts, err := p.Close()
	if err != nil || len(receipts) != 1 || !receipts[0].Closed {
		t.Fatal(receipts, err)
	}
	if exited, err := watch.Exited(); err != nil || !exited {
		t.Fatal("closed probe left descendant alive", exited, err)
	}
	again, err := p.Close()
	if err != nil || again[0] != receipts[0] {
		t.Fatal("receipt changed", again, err)
	}
}

// Both streams are drained with a fixed memory cap; overflow is ordinary probe
// failure once supervised cleanup has completed.
func TestRuntimeProbe_shouldBoundNoisyOutputAndClose(t *testing.T) {
	var b probeCapture
	chunk := []byte(strings.Repeat("x", runtimeProbeOutputLimit*2))
	n, err := b.Write(chunk)
	if err != nil || n != len(chunk) || len(b.data) != runtimeProbeOutputLimit || !b.overflow {
		t.Fatal("capture not bounded")
	}
	t.Setenv("XR_TEST_VERSION_PROBE", "noisy")
	binary, _ := os.Executable()
	p, _ := (runtimeControl{}).PrepareProbe(release.ProbeRequest{Binary: binary, Resource: release.ResourceObligation{ImageSHA256: buildinfo.Running().SHA256}})
	_, err = p.Run(context.Background())
	if err == nil || errors.Is(err, release.ErrCleanupUncertain) {
		t.Fatal(err)
	}
	receipts, err := p.Close()
	if err != nil || !receipts[0].Closed {
		t.Fatal(receipts, err)
	}
}

// Rejected image admission allocated no native image; a typed nil *os.File
// must not be interpreted as a live handle whose Close failed.
func TestRuntimeProbe_shouldCloseReceipt_whenImageIsAbsent(t *testing.T) {
	p, _ := (runtimeControl{}).PrepareProbe(release.ProbeRequest{Binary: filepath.Join(t.TempDir(), "missing"), Resource: release.ResourceObligation{ImageSHA256: strings.Repeat("a", 64)}})
	_, err := p.Run(context.Background())
	if err == nil || errors.Is(err, release.ErrCleanupUncertain) {
		t.Fatal("absence misclassified", err)
	}
	receipts, err := p.Close()
	if err != nil || !receipts[0].Closed {
		t.Fatal(receipts, err)
	}
}
