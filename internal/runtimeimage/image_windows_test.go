//go:build windows

package runtimeimage

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

// With an image admitted, actual Windows filesystem mutations must be denied
// until its handle closes. A checksum checked only before these writes fails.
func TestOpen_should_holdImageAgainstReplacement_untilClosed(t *testing.T) {
	for _, action := range []string{"overwrite", "rename", "unlink", "parent-rename", "ancestor-rename", "hardlink-write", "rooted-replace"} {
		t.Run(action, func(t *testing.T) {
			root := filepath.Join(t.TempDir(), "owner")
			dir := filepath.Join(root, "activation")
			if err := os.MkdirAll(dir, 0700); err != nil {
				t.Fatal(err)
			}
			image := filepath.Join(dir, "runtime.exe")
			bytes := []byte("expected image bytes")
			if err := os.WriteFile(image, bytes, 0700); err != nil {
				t.Fatal(err)
			}
			sum := sha256.Sum256(bytes)
			guard, err := Open(image, hex.EncodeToString(sum[:]))
			if err != nil {
				t.Fatal(err)
			}
			defer guard.Close()
			var mutate func() error
			switch action {
			case "overwrite":
				mutate = func() error { return os.WriteFile(image, []byte("replacement"), 0700) }
			case "rename":
				mutate = func() error { return os.Rename(image, image+".old") }
			case "unlink":
				mutate = func() error { return os.Remove(image) }
			case "parent-rename":
				mutate = func() error { return os.Rename(dir, dir+".old") }
			case "ancestor-rename":
				mutate = func() error { return os.Rename(root, root+".old") }
			case "hardlink-write":
				alias := filepath.Join(root, "alias.exe")
				if err := os.Link(image, alias); err != nil {
					t.Fatal(err)
				}
				mutate = func() error { return os.WriteFile(alias, []byte("replacement"), 0700) }
			case "rooted-replace":
				if err := os.WriteFile(filepath.Join(dir, "replacement.exe"), []byte("replacement"), 0700); err != nil {
					t.Fatal(err)
				}
				base, err := os.OpenRoot(dir)
				if err != nil {
					t.Fatal(err)
				}
				defer base.Close()
				mutate = func() error { return base.Rename("replacement.exe", "runtime.exe") }
			}
			if err := mutate(); err == nil {
				t.Fatal("admitted image remained mutable")
			}
			if actual, err := os.ReadFile(image); err != nil || string(actual) != string(bytes) {
				t.Fatal("refused mutation changed admitted bytes", err)
			}
			if err := guard.Close(); err != nil {
				t.Fatal(err)
			}
			if err := mutate(); err != nil {
				t.Fatal("image protection was not released", err)
			}
		})
	}
}
