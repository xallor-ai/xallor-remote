package release

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"

	"github.com/xallor-ai/xallor-remote/internal/buildinfo"
	"github.com/xallor-ai/xallor-remote/internal/runtimeimage"
)

// prepareBaseline runs under the install lock and before candidate probing or
// stopping a process. Selected activations are authoritative; the version cache
// is never a substitute for a missing or damaged selected activation.
func (s Store) prepareBaseline(ctx context.Context, st State, live *buildinfo.Executable, c Control) (State, *Activation, error) {
	if st.Current != nil {
		a, err := s.authenticatedActivation(ctx, st.CurrentActivation, *st.Current)
		if err != nil {
			return st, nil, fmt.Errorf("cannot preserve selected activation: %w", err)
		}
		if live != nil && (live.Info != st.Current.Build || live.SHA256 != st.Current.Artifact.SHA256 || !runtimeimage.MatchesPath(live.Path, a.Binary)) {
			return st, nil, errors.New("running version differs from selected activation")
		}
		if err := s.preflightActivation(c, a); err != nil {
			return st, nil, err
		}
		return st, nil, nil
	}
	if live == nil {
		return st, nil, nil
	}
	// An unmanaged running baseline can be adopted only from its original signed
	// manifest. Copy from the authenticated quarantine bytes, never reopen input
	// paths after verification to create the recovery activation.
	var adopted *Activation
	_, err := s.withSourceSnapshot(ctx, filepath.Dir(live.Path), "", func(binary string, m Manifest, raw []byte) error {
		if m.Build != live.Info || m.Artifact.SHA256 != live.SHA256 || filepath.Base(live.Path) != m.Artifact.Filename {
			return errors.New("running version differs from authenticated manifest")
		}
		a, err := s.StageActivation(ctx, binary, m, raw)
		if err != nil {
			return err
		}
		adopted = &a
		if err := s.preflightActivation(c, a); err != nil {
			return err
		}
		st.Current = &m
		st.CurrentActivation = a.ID
		return nil
	})
	if err != nil {
		return st, adopted, fmt.Errorf("cannot preserve running version: %w", err)
	}
	return st, adopted, nil
}

func (s Store) authenticatedActivation(ctx context.Context, id string, m Manifest) (Activation, error) {
	a, err := s.Activation(id, m)
	if err != nil {
		return Activation{}, err
	}
	var digest, snapshot string
	verified, err := s.withSourceSnapshot(ctx, a.Dir, "", func(_ string, _ Manifest, raw []byte) error {
		digest = rawManifestSHA(raw)
		snapshot = snapshotDigest(raw, m.Artifact.SHA256)
		return nil
	})
	if err != nil {
		return Activation{}, err
	}
	if verified != m {
		return Activation{}, errors.New("activation source differs from selected manifest")
	}
	// The provenance verifier can take time. Detect changes during verification;
	// process-launch locking remains a separate lifecycle requirement.
	a, err = s.Activation(id, m)
	a.manifestSHA256 = digest
	a.sourceSnapshotSHA256 = snapshot
	return a, err
}
