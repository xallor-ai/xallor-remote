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
// credential. Install/Stage, rollback and active selection independently
// authenticate their own snapshots; callers must not reuse this observation.
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

func (s Store) authenticateManifestDir(ctx context.Context, dir string) (Manifest, error) {
	return s.withSourceSnapshot(ctx, dir, "", nil)
}

type sourceVerifier func(context.Context, string, releasetrust.Identity, string, string) error

// consume runs only after both proofs verify and receives the private snapshot,
// never a mutable input path. verifier is private wiring for state-machine tests.
func withSourceSnapshot(ctx context.Context, bundle, proofBundle string, verifier sourceVerifier, consume func(string, Manifest, []byte) error) (result Manifest, resultErr error) {
	return trackedSourceSnapshot(ctx, bundle, proofBundle, verifier, consume, nil)
}

func trackedSourceSnapshot(ctx context.Context, bundle, proofBundle string, verifier sourceVerifier, consume func(string, Manifest, []byte) error, recorder ResourceRecorder) (result Manifest, resultErr error) {
	defer func() {
		if resultErr != nil {
			result = Manifest{}
		}
	}()
	root, err := os.OpenRoot(bundle)
	if err != nil {
		return result, err
	}
	var source *os.File
	var snapshot *sourceSnapshot
	var obligation ResourceObligation
	var recorded bool
	var allocationErr error
	var verifierCleanupErr error
	defer func() {
		var cleanup error
		if snapshot != nil {
			cleanup = snapshot.cleanup()
		}
		if source != nil {
			cleanup = errors.Join(cleanup, source.Close())
		}
		cleanup = errors.Join(cleanup, root.Close())
		// Verifier descendants belong to this acquisition. A removed snapshot
		// cannot settle their unknown lifetime; consumer errors are independent.
		cleanup = errors.Join(cleanup, verifierCleanupErr)
		// One acquisition receipt settles only after quarantine and original
		// input handles close. Pre-intent failures have no ledger receipt.
		if recorded {
			cleanup = resourceCleanup(recorder, obligation, errors.Join(cleanup, allocationErr))
		}
		resultErr = sourceCleanupError(resultErr, cleanup)
	}()
	manifest, err := openSourceFile(root, "manifest.json")
	if err != nil {
		return result, err
	}
	info, err := manifest.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return result, sourceCleanupError(errors.Join(err, errors.New("发行 manifest 必须为普通文件")), manifest.Close())
	}
	raw, err := io.ReadAll(io.LimitReader(manifest, 65537))
	err = sourceCleanupError(err, manifest.Close())
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
	source, err = openSourceFile(root, m.Artifact.Filename)
	if err != nil {
		return result, err
	}
	info, err = source.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() != m.Artifact.Size {
		return result, errors.Join(err, errors.New("发行二进制类型或大小不符"))
	}
	if recorder != nil {
		id, attempt, err := resourceIdentity()
		if err != nil {
			return result, err
		}
		obligation = ResourceObligation{ID: id, AttemptID: attempt, Generation: 1, Phase: resourcePhasePreflight, Kind: resourceKindSource,
			ImageSHA256: m.Artifact.SHA256, SourceSnapshotSHA256: snapshotDigest(raw, m.Artifact.SHA256), ObserverScope: resourceScopeOwner, Retention: resourceRelease}
		if err := recorder.RecordIntent(obligation); err != nil {
			return result, errors.Join(ErrCleanupUncertain, err)
		}
		recorded = true
	}
	snapshot, err = newSourceSnapshot()
	if err != nil {
		// Allocation may have succeeded without a receipt. Conservatively retain
		// the intent; absence cannot be established from the factory error.
		allocationErr = err
		return result, err
	}
	// Only quarantine copies occur here. The install directory is never opened.
	manifestPath := filepath.Join(snapshot.path, "manifest.json")
	manifestCopy, err := snapshot.create("manifest.json")
	if err != nil {
		return result, err
	}
	_, writeErr := manifestCopy.Write(raw)
	if err := sourceCleanupError(writeErr, snapshot.closeFile(manifestCopy)); err != nil {
		return result, err
	}
	binaryPath := filepath.Join(snapshot.path, m.Artifact.Filename)
	destination, err := snapshot.create(m.Artifact.Filename)
	if err != nil {
		return result, err
	}
	digest := sha256.New()
	n, copyErr := io.Copy(io.MultiWriter(destination, digest), io.LimitReader(source, maxBinaryBytes+1))
	closeErr := snapshot.closeFile(destination)
	if err := sourceCleanupError(copyErr, closeErr); err != nil {
		return result, err
	}
	if n != m.Artifact.Size || hex.EncodeToString(digest.Sum(nil)) != m.Artifact.SHA256 {
		return result, errors.New("发行二进制完整性校验失败")
	}
	manifestDigest := sha256.Sum256(raw)
	for _, file := range []string{manifestPath, binaryPath} {
		var err error
		if verifier != nil {
			err = verifier(ctx, file, identity, proofBundle, bundle)
		} else {
			expected := m.Artifact.SHA256
			if file == manifestPath {
				expected = hex.EncodeToString(manifestDigest[:])
			}
			err = releasetrust.Verify(ctx, file, identity, proofBundle, bundle, expected)
		}
		if errors.Is(err, releasetrust.ErrCleanupUncertain) {
			verifierCleanupErr = releasetrust.ErrCleanupUncertain
		}
		if err != nil {
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
