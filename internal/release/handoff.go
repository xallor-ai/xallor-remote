package release

import (
	"context"
	"errors"

	"github.com/xallor-ai/xallor-remote/internal/buildinfo"
	"github.com/xallor-ai/xallor-remote/internal/runtimeimage"
	"github.com/xallor-ai/xallor-remote/internal/runtimeprocess"
)

type handoffImageVerifier func(int, string, string) (runtimeprocess.ExitWatch, error)

// OwnerHandoff is ephemeral context for a private inherited pipe, never a head
// response or a reusable launch credential. The worker must authenticate it anew.
type OwnerHandoff struct {
	ParentPID     int    `json:"parent_pid"`
	OperationID   string `json:"operation_id"`
	IntentSHA256  string `json:"intent_sha256"`
	ActivationID  string `json:"activation_id"`
	OwnerLaunchID string `json:"owner_launch_id,omitempty"`
	ImageSHA256   string `json:"image_sha256"`
	ProfileSHA256 string `json:"profile_sha256"`
	StateSHA256   string `json:"state_sha256"`
}

// NewOwnerHandoff authenticates the currently selected serving image. The
// returned activation is private owner data, never executable authority for a head.
func (s Store) NewOwnerHandoff(ctx context.Context, owner buildinfo.Executable, operationID, intentSHA string) (OwnerHandoff, Activation, error) {
	var empty OwnerHandoff
	if !observedActivationID.MatchString(operationID) || !digestPattern.MatchString(intentSHA) {
		return empty, Activation{}, errors.New("invalid lifecycle operation binding")
	}
	before, err := s.Read()
	if err != nil {
		return empty, Activation{}, err
	}
	view, err := s.ObserveRuntime(ctx, owner)
	if err != nil {
		return empty, Activation{}, err
	}
	if err := view.ValidateReady(owner.ProfileFormat, owner.Protocol); err != nil {
		return empty, Activation{}, err
	}
	if before.Current == nil || before.CurrentActivation != view.ActivationID {
		return empty, Activation{}, errors.New("owner selection changed")
	}
	activation, err := s.Activation(view.ActivationID, *before.Current)
	if err != nil {
		return empty, Activation{}, err
	}
	profile, err := s.launchProfileDigest()
	if err != nil {
		return empty, Activation{}, err
	}
	after, err := s.Read()
	if err != nil {
		return empty, Activation{}, err
	}
	if launchStateDigest(before) != launchStateDigest(after) {
		return empty, Activation{}, errors.New("owner selection changed during handoff authentication")
	}
	if err := ctx.Err(); err != nil {
		return empty, Activation{}, err
	}
	return OwnerHandoff{ParentPID: owner.PID, OperationID: operationID, IntentSHA256: intentSHA,
		ActivationID: view.ActivationID, OwnerLaunchID: view.LaunchID, ImageSHA256: owner.SHA256, ProfileSHA256: profile, StateSHA256: launchStateDigest(after)}, activation, nil
}

// AuthorizeOwnerHandoff performs no journal writes, candidate probes or process
// effects. actualParent is the worker's OS parent; peerPID comes from kernel IPC
// credentials on the same connection that produced owner. The caller retains a
// kernel parent-exit observer through authentication and checks it before admit.
func (s Store) AuthorizeOwnerHandoff(ctx context.Context, permit OwnerHandoff, worker, owner buildinfo.Executable,
	actualParent, peerPID int, operationID, intentSHA string) (resultErr error) {
	if actualParent <= 0 || peerPID != actualParent || permit.ParentPID != actualParent || owner.PID != actualParent || worker.PID <= 0 || worker.PID == actualParent {
		return errors.New("lifecycle handoff parent is not the serving IPC owner")
	}
	if permit.OperationID != operationID || permit.IntentSHA256 != intentSHA || worker.SHA256 != permit.ImageSHA256 || worker.Info != owner.Info || worker.OwnerProtocol != buildinfo.RuntimeOwnerProtocol {
		return errors.New("lifecycle handoff does not match the operation or worker image")
	}
	fresh, activation, err := s.NewOwnerHandoff(ctx, owner, operationID, intentSHA)
	if err != nil {
		return err
	}
	if fresh != permit {
		return errors.New("lifecycle handoff authority changed")
	}
	if !runtimeimage.MatchesPath(worker.Path, activation.Binary) {
		return errors.New("lifecycle worker is outside the selected activation")
	}
	verifyImage := s.handoffImageVerifier
	if verifyImage == nil {
		verifyImage = runtimeprocess.WatchImage
	}
	parent, err := verifyImage(actualParent, activation.Binary, permit.ImageSHA256)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, parent.Close()) }()
	if exited, err := parent.Exited(); err != nil || exited {
		return errors.Join(errors.New("lifecycle owner exited during authentication"), err)
	}
	latest, err := s.Read()
	if err != nil {
		return err
	}
	if launchStateDigest(latest) != permit.StateSHA256 {
		return errors.New("owner selection changed during image observation")
	}
	return nil
}

// CheckOwnerHandoffState is a post-authentication check under lifecycle admission,
// not a source verifier and not authority for a new or resumed process launch.
func (s Store) CheckOwnerHandoffState(permit OwnerHandoff) error {
	profile, err := s.launchProfileDigest()
	if err != nil {
		return err
	}
	state, err := s.Read()
	if err != nil {
		return err
	}
	if profile != permit.ProfileSHA256 || launchStateDigest(state) != permit.StateSHA256 {
		return errors.New("owner handoff selection or profile changed before admission")
	}
	return nil
}
