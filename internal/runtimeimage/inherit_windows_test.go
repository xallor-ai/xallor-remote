//go:build windows

package runtimeimage

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

// A short-lived launcher may release its handle after startup. The child must
// retain the exact admitted handle, and OS protection must end after child exit.
func TestPrepare_should_inheritProtection_untilChildExit(t *testing.T) {
	source, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	bytes, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(t.TempDir(), "activation")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "runtime.exe")
	if err := os.WriteFile(path, bytes, 0700); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(bytes)
	digest := hex.EncodeToString(sum[:])
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, "-test.run=^TestInheritedImageChild$")
	image, err := Prepare(cmd, digest)
	if err != nil {
		t.Fatal(err)
	}
	defer image.Close()
	cmd.Env = append(os.Environ(), "XR_TEST_IMAGE_CHILD=1", fmt.Sprintf("XR_TEST_IMAGE_HANDLE=%d", image.Fd()), "XR_TEST_IMAGE_DIGEST="+digest)
	input, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	output, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if cmd.ProcessState == nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
	}()
	if err := image.Close(); err != nil {
		t.Fatal(err)
	}
	ready, err := bufio.NewReader(output).ReadString('\n')
	if err != nil || ready != "protected\n" {
		t.Fatalf("child did not retain admitted image: %q %v", ready, err)
	}
	if err := os.WriteFile(path, []byte("replacement"), 0700); err == nil {
		t.Fatal("child image was writable after parent handle closed")
	}
	if err := os.Rename(dir, dir+".old"); err == nil {
		t.Fatal("child image directory was replaceable")
	}
	if err := input.Close(); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Wait(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("after-exit"), 0700); err != nil {
		t.Fatal("image protection leaked beyond process exit", err)
	}
}

// Run only as a helper child; digest the inherited descriptor itself so merely
// reopening the executable by pathname cannot satisfy the inheritance check.
func TestInheritedImageChild(t *testing.T) {
	if os.Getenv("XR_TEST_IMAGE_CHILD") != "1" {
		return
	}
	fd, err := strconv.ParseUint(os.Getenv("XR_TEST_IMAGE_HANDLE"), 10, 64)
	if err != nil {
		os.Exit(31)
	}
	image := os.NewFile(uintptr(fd), "inherited-runtime")
	if image == nil {
		os.Exit(32)
	}
	if _, err := image.Seek(0, io.SeekStart); err != nil {
		os.Exit(33)
	}
	hash := sha256.New()
	if _, err := io.Copy(hash, image); err != nil || hex.EncodeToString(hash.Sum(nil)) != os.Getenv("XR_TEST_IMAGE_DIGEST") {
		os.Exit(34)
	}
	fmt.Println("protected")
	_, _ = io.Copy(io.Discard, os.Stdin)
	_ = image.Close()
	os.Exit(0)
}
