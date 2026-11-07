// Package release manages checksum-verified local Runtime artifacts.
// Manifests provide integrity, not publisher authentication; callers choose trusted bundles.
package release

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/xallor-ai/xallor-remote/internal/buildinfo"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
)

const maxBinaryBytes = 256 * 1024 * 1024

var digestPattern = regexp.MustCompile("^[a-f0-9]{64}$")

type Artifact struct {
	Filename string `json:"filename"`
	SHA256   string `json:"sha256"`
	Size     int64  `json:"size"`
}
type Manifest struct {
	Schema   int            `json:"schema"`
	Build    buildinfo.Info `json:"build"`
	Artifact Artifact       `json:"artifact"`
}

func (m Manifest) validate() error {
	if m.Schema != 1 {
		return errors.New("unsupported release manifest")
	}
	if m.Build.Version == "" || m.Build.Revision == "" || m.Build.Protocol != buildinfo.Protocol {
		return errors.New("invalid build identity or incompatible protocol")
	}
	if m.Build.ProfileFormat != buildinfo.ProfileFormat() {
		return errors.New("incompatible or unknown profile format; restore a matching binary and profile backup before switching formats")
	}
	if m.Build.OS != runtime.GOOS || m.Build.Arch != runtime.GOARCH {
		return errors.New("release is for a different platform")
	}
	name := "xallor-remote"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	if m.Artifact.Filename != name || !digestPattern.MatchString(m.Artifact.SHA256) || m.Artifact.Size <= 0 || m.Artifact.Size > maxBinaryBytes {
		return errors.New("invalid release artifact")
	}
	return nil
}
func readJSON(path string, v any) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	d := json.NewDecoder(io.LimitReader(f, 65537))
	d.DisallowUnknownFields()
	if err = d.Decode(v); err != nil {
		return err
	}
	var extra any
	if err = d.Decode(&extra); err != io.EOF {
		return errors.New("unexpected trailing manifest data")
	}
	return nil
}
func (s Store) Stage(bundle string) (Manifest, error) {
	var m Manifest
	root, err := os.OpenRoot(bundle)
	if err != nil {
		return m, err
	}
	defer root.Close()
	f, err := root.Open("manifest.json")
	if err != nil {
		return m, err
	}
	d := json.NewDecoder(io.LimitReader(f, 65537))
	d.DisallowUnknownFields()
	err = d.Decode(&m)
	var extra any
	if err == nil && d.Decode(&extra) != io.EOF {
		err = errors.New("trailing manifest data")
	}
	f.Close()
	if err != nil {
		return m, err
	}
	if err = m.validate(); err != nil {
		return m, err
	}
	source, err := root.Open(m.Artifact.Filename)
	if err != nil {
		return m, err
	}
	defer source.Close()
	return m, s.copyArtifact(source, m)
}
func (s Store) copyArtifact(source *os.File, m Manifest) error {
	dest := s.Path(m)
	if err := os.MkdirAll(filepath.Dir(dest), 0700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(dest), "stage-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	n, err := io.Copy(tmp, io.LimitReader(source, maxBinaryBytes+1))
	if err == nil && n != m.Artifact.Size {
		err = fmt.Errorf("artifact size mismatch: got %d", n)
	}
	if err == nil {
		err = tmp.Sync()
	}
	closeErr := tmp.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	sum, err := buildinfo.HashFile(name)
	if err != nil {
		return err
	}
	if sum != m.Artifact.SHA256 {
		return errors.New("artifact checksum mismatch")
	}
	// Validate the submitted bytes even when this digest is already cached.
	if _, err := os.Lstat(dest); err == nil {
		return s.Verify(m)
	} else if !os.IsNotExist(err) {
		return err
	}
	if err = os.Chmod(name, 0700); err != nil {
		return err
	}
	return os.Rename(name, dest)
}
func (s Store) Verify(m Manifest) error {
	if err := m.validate(); err != nil {
		return err
	}
	st, err := os.Lstat(s.Path(m))
	if err != nil {
		return err
	}
	if !st.Mode().IsRegular() || st.Size() != m.Artifact.Size {
		return errors.New("installed artifact is not a regular file or size changed")
	}
	sum, err := buildinfo.HashFile(s.Path(m))
	if err != nil {
		return err
	}
	if sum != m.Artifact.SHA256 {
		return errors.New("installed artifact checksum mismatch")
	}
	return nil
}
