package cli

import (
	"errors"
	"path/filepath"
	"strings"

	"github.com/xallor-ai/xallor-remote/internal/runtimeimage"
)

// bootstrapSourceDir locates metadata only; Store.EnsureRunning must still
// authenticate the original manifest and binary before activation.
func bootstrapSourceDir(executable, argv0 string) (string, error) {
	if strings.HasPrefix(executable, "/memfd:") {
		if !filepath.IsAbs(argv0) || filepath.Base(argv0) != "xallor-remote" || !runtimeimage.MatchesPath(executable, argv0) {
			return "", errors.New("protected bootstrap has no matching metadata location")
		}
		return filepath.Dir(argv0), nil
	}
	return filepath.Dir(executable), nil
}
