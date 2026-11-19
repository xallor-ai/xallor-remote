//go:build linux

package runtimeimage

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/xallor-ai/xallor-remote/internal/buildinfo"
	"golang.org/x/sys/unix"
)

// A Linux execution image must be sealed against writes, resizing and shared
// writable mappings, including through a newly opened descriptor to the inode.
func TestOpen_should_sealLinuxImage_againstMutationThroughOtherDescriptors(t *testing.T) {
	source := filepath.Join(t.TempDir(), "runtime")
	data := []byte("verified-image")
	if err := os.WriteFile(source, data, 0700); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	image, err := Open(source, hex.EncodeToString(sum[:]))
	if err != nil {
		t.Fatal(err)
	}
	defer image.Close()
	want := unix.F_SEAL_WRITE | unix.F_SEAL_GROW | unix.F_SEAL_SHRINK | unix.F_SEAL_SEAL
	seals, err := unix.FcntlInt(image.Fd(), unix.F_GET_SEALS, 0)
	if err != nil || seals&want != want {
		t.Fatalf("image is not kernel-sealed: seals=%x err=%v", seals, err)
	}
	// Duplicating the original O_RDWR memfd bypasses its now-read-only mode
	// bits. EPERM below must therefore come from seals, not ordinary access.
	aliasFD, err := unix.Dup(int(image.Fd()))
	if err != nil {
		t.Fatal(err)
	}
	alias := os.NewFile(uintptr(aliasFD), "writable-alias")
	defer alias.Close()
	if _, err := alias.WriteAt([]byte("changed"), 0); !errors.Is(err, unix.EPERM) {
		t.Fatal("sealed image accepted a write", err)
	}
	for _, size := range []int64{1, 1024} {
		if err := alias.Truncate(size); !errors.Is(err, unix.EPERM) {
			t.Fatal("sealed image accepted resize", size, err)
		}
	}
	if mapping, err := unix.Mmap(int(alias.Fd()), 0, len(data), unix.PROT_READ|unix.PROT_WRITE, unix.MAP_SHARED); !errors.Is(err, unix.EPERM) {
		if err == nil {
			_ = unix.Munmap(mapping)
		}
		t.Fatal("sealed image accepted a writable mapping", err)
	}
	if err := os.WriteFile(source, []byte("replaced source"), 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := image.Seek(0, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(image)
	if err != nil || string(got) != string(data) {
		t.Fatal("source mutation reached sealed image", string(got), err)
	}
}

// Replacing the source pathname after admission must not change the executed
// program. The real child reports its loaded image hash, not the replaced path.
func TestPrepare_should_executeSealedImage_whenLinuxSourceIsReplaced(t *testing.T) {
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(self)
	if err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(t.TempDir(), "runtime")
	if err := os.WriteFile(source, data, 0700); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	digest := hex.EncodeToString(sum[:])
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, source, "-test.run=^TestLinuxRuntimeImageChild$")
	extra, err := os.CreateTemp(t.TempDir(), "existing-extra-file")
	if err != nil {
		t.Fatal(err)
	}
	defer extra.Close()
	if _, err := extra.WriteString("preserved-extra-file"); err != nil {
		t.Fatal(err)
	}
	if _, err := extra.Seek(0, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	cmd.ExtraFiles = []*os.File{extra}
	cmd.Env = append(os.Environ(), "XR_TEST_LINUX_IMAGE_CHILD=1", "XR_TEST_EXTRA_FILE=1")
	image, err := Prepare(cmd, digest)
	if err != nil {
		t.Fatal(err)
	}
	defer image.Close()
	if err := os.Rename(source, source+".old"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(source, []byte("replacement must not execute"), 0700); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	cmd.Stdout = &output
	if err := cmd.Start(); err != nil {
		t.Fatalf("did not execute admitted image after source replacement: %v", err)
	}
	// The child owns its descriptor after Start. Closing the launcher's copy
	// cannot invalidate execution or loaded-image observation.
	if err := image.Close(); err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		t.Fatal(err)
	}
	if err := cmd.Wait(); err != nil {
		t.Fatal(err)
	}
	var got buildinfo.Executable
	if err := json.Unmarshal(output.Bytes(), &got); err != nil {
		t.Fatal(err, output.String())
	}
	if got.SHA256 != digest || got.PID != cmd.Process.Pid || !strings.HasPrefix(got.Path, "/memfd:") {
		t.Fatalf("loaded image identity is not preserved: %+v", got)
	}
	if !MatchesPath(got.Path, source) || MatchesPath(got.Path, source+".other-activation") {
		t.Fatal("loaded image was not bound to its selected activation")
	}
}

// This helper is only entered in an admitted child process; the JSON is the
// production identity observation of the actual loaded executable.
func TestLinuxRuntimeImageChild(t *testing.T) {
	if os.Getenv("XR_TEST_LINUX_IMAGE_CHILD") != "1" {
		return
	}
	if os.Getenv("XR_TEST_EXTRA_FILE") == "1" {
		f := os.NewFile(3, "preserved-extra-file")
		data, err := io.ReadAll(f)
		_ = f.Close()
		if err != nil || string(data) != "preserved-extra-file" {
			os.Exit(35)
		}
	}
	_ = json.NewEncoder(os.Stdout).Encode(buildinfo.Running())
	os.Exit(0)
}
