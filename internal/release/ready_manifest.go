package release

import (
	"errors"
	"io"
	"os"
	"path/filepath"
)

// CaptureReadyManifest observes original raw bytes through bounded rooted
// regular-file reads. It never authenticates those bytes or grants authority.
func CaptureReadyManifest(binary string, expected Manifest) (manifestSHA, sourceSHA string, resultErr error) {
	root, err := os.OpenRoot(filepath.Dir(binary))
	if err != nil {
		return "", "", err
	}
	defer func() { resultErr = sourceCleanupError(resultErr, root.Close()) }()
	file, err := openSourceFile(root, "manifest.json")
	if err != nil {
		return "", "", err
	}
	defer func() { resultErr = sourceCleanupError(resultErr, file.Close()) }()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > 65536 {
		return "", "", errors.Join(err, errors.New("invalid ready manifest file"))
	}
	raw, err := io.ReadAll(io.LimitReader(file, 65537))
	if err != nil {
		return "", "", err
	}
	var actual Manifest
	if len(raw) > 65536 || decodeActivationManifest(raw, &actual) != nil || actual != expected {
		return "", "", errors.New("ready manifest differs from selection")
	}
	return rawManifestSHA(raw), snapshotDigest(raw, expected.Artifact.SHA256), nil
}
