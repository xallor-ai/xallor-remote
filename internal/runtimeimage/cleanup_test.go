package runtimeimage

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A failed preparation that also cannot confirm its descriptor cleanup must
// retain a distinguishable uncertainty marker for lifecycle callers.
func TestOpenFailurePreservesCleanupUncertainty(t *testing.T) {
	path := filepath.Join(t.TempDir(), "image")
	if err := os.WriteFile(path, []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	_, err = openWith(path, strings.Repeat("a", 64), func(string) (*os.File, error) { return f, nil })
	if !errors.Is(err, ErrCleanupUncertain) {
		t.Fatal("preparation lost cleanup uncertainty", err)
	}
}
