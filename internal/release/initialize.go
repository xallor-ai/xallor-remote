package release

import (
	"context"
	"errors"
	"time"
)

// InitializeOwner is the fresh-profile bootstrap exception, not an installer or
// upgrade path. It authenticates one requested snapshot and refuses any existing
// or concurrently selected owner instead of claiming that image was installed.
func (s Store) InitializeOwner(bundle string, c Control) error {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	state, err := s.Read()
	if err != nil {
		return err
	}
	if err := checkFreshSelection(state); err != nil {
		return err
	}
	_, err = s.withSourceSnapshot(ctx, bundle, "", func(binary string, m Manifest, raw []byte) error {
		return s.ensureSelected(ctx, s.recordControl(c), &bootstrapSource{binary: binary, manifest: m, raw: raw, requireFresh: true})
	})
	return err
}

func checkFreshSelection(state State) error {
	if state.Current != nil || state.Previous != nil || state.Pending != nil || state.CurrentActivation != "" || state.PreviousActivation != "" || state.PendingActivation != "" || state.CurrentLaunchID != "" || state.PendingLaunchID != "" {
		return errors.New("Runtime already has an installation selection; initialization cannot replace it")
	}
	return nil
}
