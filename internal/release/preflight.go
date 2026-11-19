package release

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/xallor-ai/xallor-remote/internal/buildinfo"
)

// Source authentication precedes this protected execution probe. A matching
// artifact identity without the owner contract is not a runnable replacement
// or recovery baseline. Call before Stop and before recording a new pending.
func (s Store) preflightActivation(c Control, a Activation) error {
	if !evidenceSHA.MatchString(a.manifestSHA256) || !evidenceSHA.MatchString(a.sourceSnapshotSHA256) {
		return errors.Join(ErrCleanupUncertain, errors.New("missing authenticated probe source identity"))
	}
	id, attempt, err := resourceIdentity()
	if err != nil {
		return err
	}
	// Probe snapshots bind the authenticated manifest bytes and artifact digest;
	// they describe a version process, not a future service launch.
	r := ProbeRequest{Binary: a.Binary, ManifestSHA256: a.manifestSHA256, Resource: ResourceObligation{ID: id, AttemptID: attempt, Generation: 1, Phase: resourcePhasePreflight, Kind: "probe", ActivationID: a.ID, ImageSHA256: a.Manifest.Artifact.SHA256, SourceSnapshotSHA256: a.sourceSnapshotSHA256, ObserverScope: resourceScopeOwner, Retention: resourceRelease}}
	if s.resources != nil {
		if err := s.resources.RecordIntent(r.Resource); err != nil {
			return errors.Join(ErrCleanupUncertain, err)
		}
	}
	prepared, prepareErr := c.PrepareProbe(r)
	var info buildinfo.Executable
	runErr := prepareErr
	closeErr := error(ErrCleanupUncertain)
	if prepared != nil {
		if prepareErr == nil {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			info, runErr = prepared.Run(ctx)
			cancel()
		}
		closures, err := prepared.Close()
		if len(closures) == 1 && closures[0].Resource == r.Resource && closures[0].Closed && err == nil {
			closeErr = nil
		} else {
			closeErr = errors.Join(closeErr, err)
		}
	}
	closeErr = resourceCleanup(s.resources, r.Resource, closeErr)
	err = errors.Join(runErr, closeErr)
	if err != nil {
		return fmt.Errorf("candidate cannot report compatible runtime identity: %w", err)
	}
	if info.Info != a.Manifest.Build || info.SHA256 != a.Manifest.Artifact.SHA256 {
		return errors.New("candidate identity does not match manifest")
	}
	if info.OwnerProtocol != buildinfo.RuntimeOwnerProtocol {
		return errors.New("candidate does not support the required Runtime owner protocol")
	}
	return nil
}
