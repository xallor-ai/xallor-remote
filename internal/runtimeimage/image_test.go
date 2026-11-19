package runtimeimage

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// A failed digest check releases every opened resource; callers can repair the
// rejected image, and no admission error changes its bytes or starts a process.
func TestOpen_should_releaseHandle_whenDigestIsRejected(t *testing.T) {
	path := filepath.Join(t.TempDir(), "image")
	if err := os.WriteFile(path, []byte("wrong-content"), 0700); err != nil {
		t.Fatal(err)
	}
	for _, digest := range []string{"", "invalid", strings.Repeat("0", 64)} {
		f, err := Open(path, digest)
		if err == nil || f != nil {
			if f != nil {
				_ = f.Close()
			}
			t.Fatal("invalid image admitted")
		}
		if err := os.WriteFile(path, []byte("wrong-content"), 0700); err != nil {
			t.Fatal("rejected image retained an open lock", err)
		}
	}
}

// Invalid executable bytes may match a digest but still fail OS startup. The
// caller releases the admitted descriptor on that error, allowing later repair.
func TestPrepare_should_allowRelease_afterProcessStartFails(t *testing.T) {
	path := filepath.Join(t.TempDir(), "invalid.exe")
	bytes := []byte("not an executable")
	if err := os.WriteFile(path, bytes, 0700); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(bytes)
	cmd := exec.Command(path)
	image, err := Prepare(cmd, hex.EncodeToString(sum[:]))
	if err != nil {
		t.Fatal(err)
	}
	defer image.Close()
	if err := cmd.Start(); err == nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		t.Fatal("invalid executable started")
	}
	if err := image.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("replacement"), 0700); err != nil {
		t.Fatal("failed-start handle retained", err)
	}
}
