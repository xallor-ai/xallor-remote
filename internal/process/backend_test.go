package process

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// Selection and requested-vs-configured matching must reject reserved/unknown
// names, including attempts to weaken an unavailable target configuration.
func TestBackend_shouldFailClosedWithoutNativeFallback(t *testing.T) {
	for _, name := range []string{"low_privilege", "cgroup", "container", "typo", "NATIVE_USER", " "} {
		t.Run(name, func(t *testing.T) {
			if b, err := Select(name); b != nil || !errors.Is(err, ErrBackendUnavailable) {
				t.Fatalf("unsupported backend selected: %v %v", b, err)
			}
			if b, err := Resolve("", name); b != nil || !errors.Is(err, ErrBackendUnavailable) {
				t.Fatalf("request fell back to native: %v %v", b, err)
			}
			if b, err := Resolve(name, NativeUser); b != nil || !errors.Is(err, ErrBackendUnavailable) {
				t.Fatalf("target configuration bypassed: %v %v", b, err)
			}
		})
	}
	for _, names := range [][2]string{{"", ""}, {NativeUser, ""}, {"", NativeUser}, {NativeUser, NativeUser}} {
		b, err := Resolve(names[0], names[1])
		if err != nil || b.Capability() != Describe() {
			t.Fatalf("native selection: %+v %v", names, err)
		}
	}
}

// The selected provider must retain real output, nonzero exit and idempotent
// cleanup semantics rather than introducing a parallel process lifecycle.
func TestBackend_shouldPreserveNativeSupervisionAndExit(t *testing.T) {
	b, err := Select(NativeUser)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := helper("output")
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	child, err := b.Start(ctx, cmd)
	if err != nil {
		t.Fatal(err)
	}
	var exit *exec.ExitError
	if err := child.Wait(); !errors.As(err, &exit) || exit.ExitCode() != 7 || errors.Is(err, ErrCleanup) {
		t.Fatal("provider changed terminal", err)
	}
	if stdout.String() != strings.Repeat("中", 40000) || stderr.String() != "stderr preserved" || child.Stop() != nil {
		t.Fatal("provider changed output/cleanup")
	}
}
