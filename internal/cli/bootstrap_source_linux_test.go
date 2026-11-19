//go:build linux

package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
	"testing"
)

// A sealed bootstrap has no disk executable pathname. Only its matching
// diagnostic argv0 locates the bundle; mismatched hints cannot select a source.
func TestBootstrapSource_should_locateSealedImageMetadata_withoutTreatingLabelAsProof(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "xallor-remote")
	hash := sha256.Sum256([]byte(binary))
	image := "/memfd:xr-runtime-" + hex.EncodeToString(hash[:])
	dir, err := bootstrapSourceDir(image, binary)
	if err != nil || dir != filepath.Dir(binary) {
		t.Fatalf("sealed bootstrap lost metadata source: %q %v", dir, err)
	}
	for _, invalid := range []string{"", "xallor-remote", binary + "-other"} {
		if _, err := bootstrapSourceDir(image, invalid); err == nil {
			t.Errorf("unbound metadata hint accepted: %q", invalid)
		}
	}
}
