package release

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/xallor-ai/xallor-remote/internal/buildinfo"
	"github.com/xallor-ai/xallor-remote/internal/releasetrust"
)

// SourceVerification is an observation of one private snapshot, not an install
// credential. Install/rollback/vendor selection do not yet consume this result.
type SourceVerification struct {
	Verified   bool           `json:"verified"`
	Repository string         `json:"repository"`
	Workflow   string         `json:"workflow"`
	SourceRef  string         `json:"source_ref"`
	VerifiedAt string         `json:"verified_at"`
	Build      buildinfo.Info `json:"build"`
	Artifact   Artifact       `json:"artifact"`
}

// VerifyBundleSource reads the bundle through OpenRoot and verifies a private
// snapshot without executing candidates, touching install state or returning an
// authenticated mutable source path. Success cannot authorize a later Install.
func VerifyBundleSource(ctx context.Context, bundle, proofBundle string) (SourceVerification, error) {
	m, err := withSourceSnapshot(ctx, bundle, proofBundle, nil, nil)
	if err != nil {
		return SourceVerification{}, err
	}
	return SourceVerification{Verified: true, Repository: releasetrust.Repository, Workflow: releasetrust.Workflow,
		SourceRef: "refs/tags/v" + m.Build.Version, VerifiedAt: time.Now().UTC().Format(time.RFC3339), Build: m.Build, Artifact: m.Artifact}, nil
}

type sourceVerifier func(context.Context, string, releasetrust.Identity, string, string) error

// consume runs only after both proofs verify and receives the private snapshot,
// never a mutable input path. verifier is private wiring for state-machine tests.
func withSourceSnapshot(ctx context.Context, bundle, proofBundle string, verifier sourceVerifier, consume func(string, Manifest, []byte) error) (Manifest, error) {
	var result Manifest
	root, err := os.OpenRoot(bundle)
	if err != nil {
		return result, err
	}
	defer root.Close()
	manifest, err := root.Open("manifest.json")
	if err != nil {
		return result, err
	}
	info, err := manifest.Stat()
	if err != nil || !info.Mode().IsRegular() {
		manifest.Close()
		return result, errors.New("发行 manifest 必须为普通文件")
	}
	raw, err := io.ReadAll(io.LimitReader(manifest, 65537))
	manifest.Close()
	if err != nil {
		return result, err
	}
	if len(raw) > 65536 {
		return result, errors.New("发行 manifest 超过大小上限")
	}
	var m Manifest
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&m); err != nil {
		return result, err
	}
	if decoder.Decode(new(any)) != io.EOF {
		return result, errors.New("发行 manifest 含额外数据")
	}
	if err := m.validate(); err != nil {
		return result, err
	}
	identity := releasetrust.Identity{Version: m.Build.Version, Revision: m.Build.Revision, Dirty: m.Build.Dirty}
	if err := identity.Validate(); err != nil {
		return result, err
	}
	if proofBundle != "" {
		proofBundle, err = filepath.Abs(proofBundle)
		if err != nil {
			return result, err
		}
	}
	source, err := root.Open(m.Artifact.Filename)
	if err != nil {
		return result, err
	}
	defer source.Close()
	info, err = source.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() != m.Artifact.Size {
		return result, errors.New("发行二进制类型或大小不符")
	}
	snapshot, err := os.MkdirTemp("", "xr-provenance-")
	if err != nil {
		return result, err
	}
	defer os.RemoveAll(snapshot)
	// Only quarantine copies occur here. The install directory is never opened.
	manifestPath := filepath.Join(snapshot, "manifest.json")
	if err := os.WriteFile(manifestPath, raw, 0600); err != nil {
		return result, err
	}
	binaryPath := filepath.Join(snapshot, m.Artifact.Filename)
	destination, err := os.OpenFile(binaryPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return result, err
	}
	digest := sha256.New()
	n, copyErr := io.Copy(io.MultiWriter(destination, digest), io.LimitReader(source, maxBinaryBytes+1))
	closeErr := destination.Close()
	if copyErr != nil {
		return result, copyErr
	}
	if closeErr != nil {
		return result, closeErr
	}
	if n != m.Artifact.Size || hex.EncodeToString(digest.Sum(nil)) != m.Artifact.SHA256 {
		return result, errors.New("发行二进制完整性校验失败")
	}
	if verifier == nil {
		verifier = releasetrust.Verify
	}
	for _, file := range []string{manifestPath, binaryPath} {
		if err := verifier(ctx, file, identity, proofBundle, bundle); err != nil {
			return result, err
		}
	}
	if consume != nil {
		if err := consume(binaryPath, m, raw); err != nil {
			return result, err
		}
	}
	return m, nil
}
