package release

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/xallor-ai/xallor-remote/internal/buildinfo"
)

var ErrActivationRunning = errors.New("activation process is still running")

type Activation struct {
	ID, Dir, Binary      string
	Manifest             Manifest
	PID                  int
	manifestSHA256       string
	sourceSnapshotSHA256 string
	ownership            *activationOwnership
}

func (s Store) Activation(id string, m Manifest) (Activation, error) {
	if id == "" {
		return Activation{}, errors.New("missing activation id")
	}
	root, err := filepath.Abs(filepath.Join(s.Dir, "activations"))
	if err != nil {
		return Activation{}, err
	}
	dir, err := filepath.Abs(filepath.Join(root, id))
	if err != nil {
		return Activation{}, err
	}
	if !withinPath(root, dir) || filepath.Base(dir) != id {
		return Activation{}, errors.New("invalid activation id")
	}
	var parsed Manifest
	if err := readJSON(filepath.Join(dir, "manifest.json"), &parsed); err != nil || parsed != m {
		return Activation{}, errors.New("activation manifest mismatch")
	}
	binary := filepath.Join(dir, m.Artifact.Filename)
	info, err := os.Stat(binary)
	if err != nil || !info.Mode().IsRegular() || info.Size() != m.Artifact.Size {
		return Activation{}, errors.New("activation artifact invalid")
	}
	if sum, err := buildinfo.HashFile(binary); err != nil || sum != m.Artifact.SHA256 {
		return Activation{}, errors.New("activation artifact checksum mismatch")
	}
	return Activation{ID: id, Dir: dir, Binary: binary, Manifest: m}, nil
}

func (s Store) StageActivation(ctx context.Context, binary string, m Manifest, raw []byte) (result Activation, resultErr error) {
	var created Activation
	// Registered before handle closers so cleanup sees every close failure.
	defer func() {
		if resultErr != nil && created.ownership != nil {
			resultErr = s.discardUnstartedActivation(created, resultErr)
		}
	}()
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if err := m.validate(); err != nil {
		return result, err
	}
	source, err := os.Open(binary)
	if err != nil {
		return result, err
	}
	defer func() { resultErr = sourceCleanupError(resultErr, source.Close()) }()
	info, err := source.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return result, errors.New("activation binary must be a regular file")
	}
	if info.Size() != m.Artifact.Size {
		return result, errors.New("activation binary size mismatch")
	}
	h := sha256.New()
	if _, err = io.Copy(h, io.LimitReader(source, maxBinaryBytes+1)); err != nil {
		return result, err
	}
	if hex.EncodeToString(h.Sum(nil)) != m.Artifact.SHA256 {
		return result, errors.New("activation binary checksum mismatch")
	}
	var parsed Manifest
	if len(raw) == 0 || decodeActivationManifest(raw, &parsed) != nil || parsed != m {
		return result, errors.New("activation manifest does not match selected state")
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	id, err := activationID()
	if err != nil {
		return result, err
	}
	root, err := filepath.Abs(filepath.Join(s.Dir, "activations"))
	if err != nil {
		return result, err
	}
	dir := filepath.Join(root, id)
	obligation, err := s.prepareActivationResource(id, m, raw)
	if err != nil {
		return result, err
	}
	if err := os.MkdirAll(root, 0700); err != nil {
		return result, errors.Join(err, resourceCleanup(s.resources, obligation, nil))
	}
	if err := os.Mkdir(dir, 0700); err != nil {
		return result, errors.Join(err, resourceCleanup(s.resources, obligation, nil))
	}
	info, err = os.Lstat(dir)
	if err != nil {
		return result, errors.Join(err, resourceCleanup(s.resources, obligation, err))
	}
	created = Activation{manifestSHA256: rawManifestSHA(raw), sourceSnapshotSHA256: snapshotDigest(raw, m.Artifact.SHA256), ID: id, Dir: dir, Binary: filepath.Join(dir, m.Artifact.Filename), Manifest: m,
		ownership: &activationOwnership{obligation: obligation, path: dir, directory: info, files: make(map[string]os.FileInfo)}}
	target, err := os.OpenRoot(dir)
	if err != nil {
		return result, err
	}
	defer func() { resultErr = sourceCleanupError(resultErr, target.Close()) }()
	opened, err := target.Lstat(".")
	if err != nil || !os.SameFile(opened, info) {
		return result, errors.New("activation directory changed while opening")
	}
	dest, err := target.OpenFile(m.Artifact.Filename, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0700)
	if err != nil {
		return result, err
	}
	created.ownership.files[m.Artifact.Filename], err = dest.Stat()
	if err != nil {
		return result, sourceCleanupError(err, dest.Close())
	}
	if _, err := source.Seek(0, io.SeekStart); err != nil {
		return result, sourceCleanupError(err, dest.Close())
	}
	copiedDigest := sha256.New()
	n, copyErr := io.Copy(io.MultiWriter(dest, copiedDigest), io.LimitReader(source, maxBinaryBytes+1))
	if copyErr == nil {
		copyErr = dest.Sync()
	}
	closeErr := dest.Close()
	if copyErr != nil || closeErr != nil || n != m.Artifact.Size {
		return result, sourceCleanupError(fmt.Errorf("activation copy failed: %w", copyErr), closeErr)
	}
	if hex.EncodeToString(copiedDigest.Sum(nil)) != m.Artifact.SHA256 {
		return result, errors.New("activation copy checksum mismatch")
	}
	manifest, err := target.OpenFile("manifest.json", os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return result, err
	}
	created.ownership.files["manifest.json"], err = manifest.Stat()
	if err != nil {
		return result, sourceCleanupError(err, manifest.Close())
	}
	_, err = manifest.Write(raw)
	if err == nil {
		err = manifest.Sync()
	}
	closeErr = manifest.Close()
	if err != nil || closeErr != nil {
		return result, sourceCleanupError(err, closeErr)
	}
	return created, nil
}
func activationID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}
func withinPath(root, candidate string) bool {
	rel, err := filepath.Rel(root, candidate)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)) && !filepath.IsAbs(rel)
}
func decodeActivationManifest(raw []byte, m *Manifest) error {
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(m); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return errors.New("trailing manifest data")
	}
	return nil
}
