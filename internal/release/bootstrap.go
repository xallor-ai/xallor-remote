package release

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/xallor-ai/xallor-remote/internal/buildinfo"
	"github.com/xallor-ai/xallor-remote/internal/runtimeimage"
)

type bootstrapSource struct {
	binary       string
	manifest     Manifest
	raw          []byte
	requireFresh bool
}

// EnsureRunning is the shared Go bootstrap/selection path. It never upgrades or
// stops a running owner, and never derives expected identity by hashing a mutable
// execution pathname. A missing selection requires a source-verified bundle.
func (s Store) EnsureRunning(bootstrapBundle string, c Control) error {
	c = s.recordControl(c)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	live, err := c.Running()
	if err != nil {
		return err
	}
	if live != nil {
		return s.observeExistingOwner(ctx, *live)
	}
	st, err := s.Read()
	if err != nil {
		return err
	}
	if err := checkBootstrapState(st); err != nil {
		return err
	}
	if st.Current != nil {
		return s.ensureSelected(ctx, c, nil)
	}
	if bootstrapBundle == "" {
		return errors.New("no verified Runtime bootstrap bundle is available")
	}
	// Authenticate before creating an install directory, executing a version
	// probe or persisting anything. The consumer receives quarantine bytes only.
	_, err = s.withSourceSnapshot(ctx, bootstrapBundle, "", func(binary string, m Manifest, raw []byte) error {
		return s.ensureSelected(ctx, c, &bootstrapSource{binary: binary, manifest: m, raw: raw})
	})
	return err
}

func checkBootstrapState(st State) error {
	if st.Pending != nil || st.PendingActivation != "" {
		return errors.New("Runtime startup or installation is pending; inspect status and run runtime rollback")
	}
	if st.Current == nil && (st.CurrentActivation != "" || st.Previous != nil || st.PreviousActivation != "") {
		return errors.New("Runtime selection is incomplete; refusing bootstrap fallback")
	}
	return nil
}

func (s Store) observeExistingOwner(ctx context.Context, live buildinfo.Executable) error {
	view, err := s.ObserveRuntime(ctx, live)
	if err != nil {
		return err
	}
	return view.ValidateReady(buildinfo.ProfileFormat(), buildinfo.Protocol)
}

func (s Store) ensureSelected(ctx context.Context, c Control, source *bootstrapSource) (resultErr error) {
	if guard := s.bootstrapGuard; guard != nil {
		s.bootstrapGuard = nil
		return guard(func() error { return s.ensureSelected(ctx, c, source) })
	}
	unlock, err := s.Lock()
	if err != nil {
		return err
	}
	defer unlock()
	live, err := c.Running()
	if err != nil {
		return err
	}
	if live != nil {
		if source != nil && source.requireFresh {
			return errors.New("a Runtime appeared before initialization start; requested image was not installed")
		}
		return s.observeExistingOwner(ctx, *live)
	}
	st, err := s.Read()
	if err != nil {
		return err
	}
	if err := checkBootstrapState(st); err != nil {
		return err
	}
	if source != nil && source.requireFresh {
		if err := checkFreshSelection(st); err != nil {
			return err
		}
	}
	var activation Activation
	defer func() { resultErr = s.discardUnstartedActivation(activation, resultErr) }()
	if st.Current != nil {
		activation, err = s.authenticatedActivation(ctx, st.CurrentActivation, *st.Current)
	} else if source != nil {
		activation, err = s.StageActivation(ctx, source.binary, source.manifest, source.raw)
	} else {
		return errors.New("Runtime selection disappeared; refusing bootstrap fallback")
	}
	if err != nil {
		return err
	}
	if err := s.preflightActivation(c, activation); err != nil {
		return err
	}
	// An owner may have appeared while provenance was checked. Its presence is
	// not permission to replace it or to bypass the owner's readiness gate.
	live, err = c.Running()
	if err != nil {
		return err
	}
	if live != nil {
		if source != nil && source.requireFresh {
			return errors.New("a Runtime appeared before initialization start; requested image was not installed")
		}
		return s.observeExistingOwner(ctx, *live)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	pending := st
	pending.Pending, pending.PendingActivation = &activation.Manifest, activation.ID
	launchID, err := activationLaunch(activation)
	if err != nil {
		return err
	}
	pending.PendingLaunchID = launchID
	if err := s.save(pending); err != nil {
		return err
	}
	// From this point every failure retains pending state. Never automatically
	// replay a start whose outcome is uncertain, or select a different binary.
	if err := c.Start(activation.Binary, activation.Manifest.Artifact.SHA256); err != nil {
		return fmt.Errorf("Runtime bootstrap did not confirm readiness; recovery required: %w", err)
	}
	live, err = c.Running()
	if err != nil {
		return err
	}
	if live == nil || live.PID <= 0 || live.Info != activation.Manifest.Build || live.SHA256 != activation.Manifest.Artifact.SHA256 || !runtimeimage.MatchesPath(live.Path, activation.Binary) {
		return errors.New("Runtime bootstrap identity was not confirmed; recovery required")
	}
	selected := st
	selected.Current, selected.CurrentActivation = &activation.Manifest, activation.ID
	selected.CurrentLaunchID = pending.PendingLaunchID
	if err := s.save(selected); err != nil {
		return err
	}
	if _, err := s.ready(c); err != nil {
		return fmt.Errorf("Runtime selection committed but acknowledgement unconfirmed: %w", err)
	}
	return s.observeExistingOwner(ctx, *live)
}
