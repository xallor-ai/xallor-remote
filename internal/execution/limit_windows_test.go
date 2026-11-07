//go:build windows

package execution

import (
	"strings"
	"testing"
)

// CreateProcess's bound includes the terminating NUL and UTF-16 encoding, not
// UTF-8 bytes or runes. Quoting expansion must also be counted before startup.
func TestNativeCommandFits_should_boundEncodedArguments(t *testing.T) {
	for _, tc := range []struct {
		name, arg string
		fits      bool
	}{
		{"exact", strings.Repeat("a", 32764), true},
		{"one over", strings.Repeat("a", 32765), false},
		{"supplementary exact", strings.Repeat("🌍", 16382), true},
		{"supplementary over", strings.Repeat("🌍", 16383), false},
		{"quoting expansion", strings.Repeat(`"`, 20000), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := nativeCommandFits("x", []string{tc.arg}); got != tc.fits {
				t.Fatalf("fits=%v want %v", got, tc.fits)
			}
		})
	}
}
