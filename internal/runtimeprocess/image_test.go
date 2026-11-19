package runtimeprocess

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"runtime"
	"strings"
	"testing"
)

// The running image must be observed independently of caller-reported metadata.
func TestWatchImageBindsActualProcessAndDigest(t *testing.T) {
	if runtime.GOOS != "windows" && runtime.GOOS != "linux" {
		t.Skip("unsupported image observation")
	}
	path, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(raw)
	w, err := WatchImage(os.Getpid(), path, hex.EncodeToString(sum[:]))
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	if exited, err := w.Exited(); err != nil || exited {
		t.Fatal(exited, err)
	}
	if other, err := WatchImage(os.Getpid(), path, strings.Repeat("0", 64)); err == nil {
		other.Close()
		t.Fatal("wrong loaded digest accepted")
	}
}
