package release

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
)

// ResourceObligation is the release-owned value passed to the Runtime owner
// ledger. IDs are receipts; digests bind the intended manifest/artifact bytes.
// Intent alone does not prove copying or provenance verification succeeded.
type ResourceObligation struct {
	ID                   string
	Phase                string
	Kind                 string
	AttemptID            string
	Generation           int64
	ActivationID         string
	LaunchID             string
	ImageSHA256          string
	SourceSnapshotSHA256 string
	ObserverScope        string
	Retention            string
}

// ResourceRecorder is deliberately owned by release so release never imports
// runtimeowner. Implementations must durably record intent before allocation;
// Close and Unknown are observations, never inferred from process exit.
type ResourceRecorder interface {
	RetainSelected(ResourceObligation, ReadyObservation) error
	RecordIntent(ResourceObligation) error
	Close(ResourceObligation) error
	Unknown(ResourceObligation, string) error
}

const (
	resourcePhasePreflight = "preflight"
	resourceKindSource     = "source_snapshot"
	resourceScopeOwner     = "owner_process"
	resourceRelease        = "release_required"
	resourceCauseUnknown   = "cleanup_unknown"
)

// WithResources attaches the admitted owner's durable ledger. Value copies made
// by WithEffects and transition preserve this recorder.
func (s Store) WithResources(recorder ResourceRecorder) Store { s.resources = recorder; return s }

// WithResourceRecorder is the explicit spelling for callers wiring an owner
// transaction; WithResources remains the concise internal spelling.
func (s Store) WithResourceRecorder(recorder ResourceRecorder) Store {
	return s.WithResources(recorder)
}

func (s Store) withSourceSnapshot(ctx context.Context, bundle, proof string, consume func(string, Manifest, []byte) error) (Manifest, error) {
	return trackedSourceSnapshot(ctx, bundle, proof, s.sourceVerifier, consume, s.resources)
}

func resourceIdentity() (id, attempt string, err error) {
	var b [32]byte
	if _, err = rand.Read(b[:]); err != nil {
		return "", "", err
	}
	return hex.EncodeToString(b[:16]), hex.EncodeToString(b[16:]), nil
}

func snapshotDigest(manifest []byte, artifact string) string {
	h := sha256.New()
	_, _ = h.Write(manifest)
	_, _ = h.Write([]byte{0})
	_, _ = h.Write([]byte(artifact))
	return hex.EncodeToString(h.Sum(nil))
}

func resourceCleanup(recorder ResourceRecorder, obligation ResourceObligation, cleanup error) error {
	if recorder == nil {
		return cleanup
	}
	if cleanup != nil {
		return errors.Join(cleanup, ErrCleanupUncertain, recorder.Unknown(obligation, resourceCauseUnknown))
	}
	if err := recorder.Close(obligation); err != nil {
		return errors.Join(ErrCleanupUncertain, err)
	}
	return nil
}
