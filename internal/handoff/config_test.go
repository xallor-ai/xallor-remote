package handoff

import (
	"os"
	"strings"
	"testing"
)

// Both initial configuration and reloaded configuration enforce the same executable/argument bounds.
func TestConfig_should_validateLocalAdapterBoundaries(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	valid := Config{Executable: executable, Prefix: []string{"entry.js"}}
	if err := Configure(t.TempDir(), valid); err != nil {
		t.Fatal(err)
	}
	for _, cfg := range []Config{
		{Executable: "relative.exe"},
		{Executable: t.TempDir()},
		{Executable: executable, Prefix: []string{strings.Repeat("x", 2049)}},
		{Executable: executable, Prefix: []string{"bad\x00argument"}},
		{Executable: executable, Prefix: []string{"1", "2", "3", "4", "5"}},
	} {
		if cfg.validate() == nil {
			t.Fatal("invalid adapter accepted")
		}
	}
}
