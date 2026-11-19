//go:build windows

package runtimeprocess

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

// Identical bytes at another pathname are not the process's original image
// section. This prevents filename replacement from becoming parent authority.
func TestWatchImageRejectsDifferentFileObjectWithIdenticalBytes(t *testing.T) {
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(self)
	if err != nil {
		t.Fatal(err)
	}
	copy := filepath.Join(t.TempDir(), "selected.exe")
	if err := os.WriteFile(copy, raw, 0600); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(raw)
	if w, err := WatchImage(os.Getpid(), copy, hex.EncodeToString(sum[:])); err == nil {
		w.Close()
		t.Fatal("different image section accepted")
	}
}
