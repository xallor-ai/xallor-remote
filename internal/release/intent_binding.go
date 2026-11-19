package release

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"github.com/xallor-ai/xallor-remote/internal/releasetrust"
)

// ResolvedIntent records authenticated bytes and the original selection. It is
// evidence, never a reusable source or launch capability. All fields are values.
type ResolvedIntent struct {
	Baseline       SelectionEvidence `json:"baseline"`
	Requested      SelectionIdentity `json:"requested"`
	Manifest       Manifest          `json:"manifest"`
	ManifestSHA256 string            `json:"manifest_sha256,omitempty"`
	ClearOnly      bool              `json:"clear_only"`
}

func (b ResolvedIntent) Valid() bool {
	if !b.Baseline.Valid() {
		return false
	}
	if b.ClearOnly {
		return b.Requested == (SelectionIdentity{}) && b.Manifest == (Manifest{}) && b.ManifestSHA256 == "" && b.Baseline.Previous == (SelectionIdentity{}) && b.Baseline.Current == (SelectionIdentity{}) && b.Baseline.Pending.SHA256 != ""
	}
	return (releasetrust.Identity{Version: b.Manifest.Build.Version, Revision: b.Manifest.Build.Revision, Dirty: b.Manifest.Build.Dirty}).Validate() == nil && b.Manifest.validate() == nil && evidenceSHA.MatchString(b.ManifestSHA256) && b.Requested.valid() && b.Requested.SHA256 == b.Manifest.Artifact.SHA256 && b.Requested.ActivationID != "" && b.Requested.LaunchID == ""
}

func resolvedIntent(st State, a Activation) ResolvedIntent {
	return ResolvedIntent{Baseline: ObserveSelection(st), Requested: selectionIdentity(&a.Manifest, a.ID, ""), Manifest: a.Manifest, ManifestSHA256: a.manifestSHA256}
}
func rawManifestSHA(raw []byte) string { sum := sha256.Sum256(raw); return hex.EncodeToString(sum[:]) }
func (s Store) bind(b ResolvedIntent) error {
	if s.effects == nil {
		return nil
	}
	if !b.Valid() {
		return errors.Join(ErrCleanupUncertain, errors.New("invalid resolved request"))
	}
	if err := s.effects.Bind(b); err != nil {
		return errors.Join(ErrCleanupUncertain, err)
	}
	return nil
}

// transition copies purpose into the Store and unwraps only our own decorator.
// Concurrent Store callers never share mutable transition context.
func (s Store) transition(p string, c Control) (Store, Control) {
	s.purpose = p
	if wrapped, ok := c.(effectControl); ok {
		c = wrapped.Control
	}
	return s, s.recordControl(c)
}
