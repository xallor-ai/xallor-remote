//go:build windows

package process

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Real child modes test OS handles, NUL semantics and environment interpretation.
func TestNativeStartupHelper(t *testing.T) {
	switch os.Getenv("XR_NATIVE_START_MODE") {
	case "event":
		h, _ := strconv.ParseUint(os.Getenv("XR_NATIVE_EVENT"), 10, 64)
		_ = windows.SetEvent(windows.Handle(h))
		os.Exit(0)
	case "null":
		data, err := io.ReadAll(os.Stdin)
		if err != nil || len(data) != 0 {
			os.Exit(2)
		}
		if _, err := os.Stdout.WriteString("discard stdout"); err != nil {
			os.Exit(3)
		}
		if _, err := os.Stderr.WriteString("discard stderr"); err != nil {
			os.Exit(4)
		}
		os.Exit(0)
	case "environment":
		if os.Getenv("XR_NATIVE_VALUE") != "new 中文" || os.Getenv("SystemRoot") == "" {
			os.Exit(5)
		}
		os.Exit(0)
	case "stdio":
		if _, err := io.Copy(os.Stdout, os.Stdin); err != nil {
			os.Exit(6)
		}
		_, _ = os.Stderr.WriteString("stderr tail")
		os.Exit(0)
	}
}

func nativeFixture(mode string) *exec.Cmd {
	cmd := exec.Command(os.Args[0], "-test.run=^TestNativeStartupHelper$")
	cmd.Env = append(os.Environ(), "XR_NATIVE_START_MODE="+mode)
	return cmd
}

func runNative(t *testing.T, cmd *exec.Cmd) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	child, err := Start(ctx, cmd)
	if err != nil {
		t.Fatal(err)
	}
	if err := child.Wait(); err != nil {
		t.Fatal(err)
	}
}

// An unrelated inheritable event must stay outside the child HANDLE_LIST; the
// child tries to signal its exact numeric handle and must not affect the parent.
func TestNative_should_excludeUnrelatedInheritableHandles(t *testing.T) {
	sa := windows.SecurityAttributes{Length: uint32(unsafe.Sizeof(windows.SecurityAttributes{})), InheritHandle: 1}
	event, err := windows.CreateEvent(&sa, 1, 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer windows.CloseHandle(event)
	cmd := nativeFixture("event")
	cmd.Env = append(cmd.Env, "XR_NATIVE_EVENT="+strconv.FormatUint(uint64(event), 10))
	runNative(t, cmd)
	if state, err := windows.WaitForSingleObject(event, 0); err != nil || state != uint32(windows.WAIT_TIMEOUT) {
		t.Fatalf("child inherited unrelated event: %d %v", state, err)
	}
}

// Omitted streams are valid NUL handles, and environment overrides retain
// os/exec's case-insensitive last-value and critical-SystemRoot semantics.
func TestNative_should_preserveNullStdioAndEnvironment(t *testing.T) {
	runNative(t, nativeFixture("null"))
	cmd := nativeFixture("environment")
	cmd.Env = append(cmd.Env, "XR_NATIVE_VALUE=old", "xr_native_value=new 中文")
	cmd.Path, cmd.Dir = filepath.Base(os.Args[0]), filepath.Dir(os.Args[0])
	runNative(t, cmd)
}

// Generic stdin and a shared stdout/stderr writer use the same atomic launch
// path, with complete output collected before Wait and no concurrent buffer writes.
func TestNative_should_copyGenericStreamsAndDrainSharedWriter(t *testing.T) {
	cmd := nativeFixture("stdio")
	want := strings.Repeat("输入 output", 12000)
	cmd.Stdin = strings.NewReader(want)
	var output bytes.Buffer
	cmd.Stdout, cmd.Stderr = &output, &output
	runNative(t, cmd)
	if output.String() != want+"stderr tail" {
		t.Fatal("generic IO lost output")
	}
}

// Native creation must honor lookup errors and reject unsupported process
// attributes or invalid environment before a child can exist.
func TestNative_should_rejectInvalidInputsBeforeCreation(t *testing.T) {
	sentinel := errors.New("lookup failed")
	for _, invalid := range []string{"lookup", "environment", "attributes"} {
		t.Run(invalid, func(t *testing.T) {
			cmd := nativeFixture("null")
			switch invalid {
			case "lookup":
				cmd.Err = sentinel
			case "environment":
				cmd.Env = append(cmd.Env, "INVALID=before\x00after")
			case "attributes":
				cmd.SysProcAttr = &syscall.SysProcAttr{NoInheritHandles: true}
			}
			child, err := Start(context.Background(), cmd)
			if err == nil || child != nil || cmd.Process != nil {
				t.Fatalf("invalid input launched: %v", err)
			}
			if invalid == "lookup" && !errors.Is(err, sentinel) {
				t.Fatal("lookup error lost")
			}
		})
	}
}
