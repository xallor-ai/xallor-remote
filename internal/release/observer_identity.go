package release

import (
	"context"
	"errors"

	"github.com/xallor-ai/xallor-remote/internal/buildinfo"
	"github.com/xallor-ai/xallor-remote/internal/runtimeimage"
	"github.com/xallor-ai/xallor-remote/internal/runtimeprocess"
)

// AuthenticateLifecycleObserver accepts the serving owner or its retained
// transaction process across replacement. It authenticates actual process bytes
// and original source proof from a stable retained activation, not a response PID.
func (s Store) AuthenticateLifecycleObserver(ctx context.Context, live buildinfo.Executable, peerPID int) (result error) {
	if peerPID <= 0 || live.PID != peerPID || live.OwnerProtocol != buildinfo.RuntimeOwnerProtocol {
		return errors.New("lifecycle observer PID/protocol mismatch")
	}
	state, err := s.Read()
	if err != nil {
		return err
	}
	var selected *Manifest
	var id string
	for _, entry := range []struct {
		manifest *Manifest
		id       string
	}{{state.Current, state.CurrentActivation}, {state.Previous, state.PreviousActivation}, {state.Pending, state.PendingActivation}} {
		if entry.manifest == nil || entry.manifest.Build != live.Info || entry.manifest.Artifact.SHA256 != live.SHA256 {
			continue
		}
		activation, err := s.Activation(entry.id, *entry.manifest)
		if err != nil || !runtimeimage.MatchesPath(live.Path, activation.Binary) {
			continue
		}
		selected, id = entry.manifest, entry.id
		break
	}
	if selected == nil {
		return errors.New("query endpoint is not a retained Runtime activation")
	}
	activation, err := s.authenticatedActivation(ctx, id, *selected)
	if err != nil {
		return err
	}
	watch, err := runtimeprocess.WatchImage(peerPID, activation.Binary, selected.Artifact.SHA256)
	if err != nil {
		return err
	}
	defer func() { result = errors.Join(result, watch.Close()) }()
	if exited, err := watch.Exited(); err != nil || exited {
		return errors.Join(errors.New("query observer exited during authentication"), err)
	}
	latest, err := s.Read()
	if err != nil {
		return err
	}
	if launchStateDigest(latest) != launchStateDigest(state) {
		return errors.New("query observer activation changed during authentication")
	}
	return ctx.Err()
}
