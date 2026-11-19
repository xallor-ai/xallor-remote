package release

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"path/filepath"

	"github.com/xallor-ai/xallor-remote/internal/buildinfo"
	"github.com/xallor-ai/xallor-remote/internal/runtimeimage"
)

// LaunchPermit is ephemeral parent-to-child bootstrap context, never persisted,
// returned to a head, or reused for retry. It is not authority by itself: the
// child verifies its actual parent, image, profile, selection and source proof.
type LaunchPermit struct {
	ParentPID     int    `json:"parent_pid"`
	ActivationID  string `json:"activation_id"`
	SHA256        string `json:"sha256"`
	StateSHA256   string `json:"state_sha256"`
	ProfileSHA256 string `json:"profile_sha256"`
	LaunchID      string `json:"launch_id,omitempty"`
}

func launchStateDigest(st State) string {
	raw, _ := json.Marshal(st)
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:])
}

func (s Store) launchProfileDigest() (string, error) {
	absolute, err := filepath.Abs(s.Dir)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256([]byte(absolute))
	return hex.EncodeToString(digest[:]), nil
}

func launchSelection(st State, id string) (*Manifest, error) {
	if st.Pending == nil || !observedActivationID.MatchString(st.PendingActivation) {
		return nil, errors.New("Runtime launch requires an active owner transition")
	}
	if id == st.PendingActivation {
		return st.Pending, nil
	}
	// A deterministic failed candidate can restore the independently verified
	// current baseline while the failed candidate's pending record is retained.
	if st.Current != nil && id == st.CurrentActivation {
		return st.Current, nil
	}
	return nil, errors.New("Runtime launch is outside the selected transition")
}

// NewLaunchPermit is called by the trusted controller under install.lock.
func (s Store) NewLaunchPermit(binary, sha string, parentPID int) (LaunchPermit, error) {
	var permit LaunchPermit
	st, err := s.Read()
	if err != nil {
		return permit, err
	}
	id := filepath.Base(filepath.Dir(binary))
	m, err := launchSelection(st, id)
	if err != nil {
		return permit, err
	}
	a, err := s.Activation(id, *m)
	if err != nil {
		return permit, err
	}
	if parentPID <= 0 || sha != m.Artifact.SHA256 || !runtimeimage.MatchesPath(binary, a.Binary) {
		return permit, errors.New("Runtime launch does not match owner selection")
	}
	profile, err := s.launchProfileDigest()
	if err != nil {
		return permit, err
	}
	return LaunchPermit{ParentPID: parentPID, ActivationID: id, SHA256: sha, StateSHA256: launchStateDigest(st), ProfileSHA256: profile, LaunchID: st.PendingLaunchID}, nil
}

// AuthorizeLaunch runs before opening identity/IPC/Relay. A pathname, stdin
// payload or a pending record alone cannot authenticate a service process.
func (s Store) AuthorizeLaunch(ctx context.Context, permit LaunchPermit, live buildinfo.Executable, actualParent int) error {
	if permit.ParentPID <= 0 || permit.ParentPID != actualParent || live.PID <= 0 || permit.ParentPID == live.PID {
		return errors.New("Runtime launch parent does not match")
	}
	profile, err := s.launchProfileDigest()
	if err != nil || profile != permit.ProfileSHA256 {
		return errors.New("Runtime launch profile does not match")
	}
	st, err := s.Read()
	if err != nil {
		return err
	}
	if launchStateDigest(st) != permit.StateSHA256 {
		return errors.New("Runtime launch selection changed")
	}
	if st.PendingLaunchID != permit.LaunchID {
		return errors.New("Runtime pending launch identity changed")
	}
	m, err := launchSelection(st, permit.ActivationID)
	if err != nil {
		return err
	}
	if permit.SHA256 != m.Artifact.SHA256 || live.SHA256 != permit.SHA256 || live.Info != m.Build || live.OwnerProtocol != buildinfo.RuntimeOwnerProtocol {
		return errors.New("Runtime launch image identity does not match")
	}
	a, err := s.authenticatedActivation(ctx, permit.ActivationID, *m)
	if err != nil {
		return err
	}
	if !runtimeimage.MatchesPath(live.Path, a.Binary) {
		return errors.New("Runtime launch image location does not match")
	}
	after, err := s.Read()
	if err != nil {
		return err
	}
	if launchStateDigest(after) != permit.StateSHA256 {
		return errors.New("Runtime launch selection changed during authentication")
	}
	return ctx.Err()
}

// LaunchSelected is a post-authentication commit gate, not a source verifier.
// IPC diagnostics may be ready while pending; business and Relay start only
// after the parent publishes this very activation as current.
func (s Store) LaunchSelected(permit LaunchPermit, live buildinfo.Executable) (bool, error) {
	st, err := s.Read()
	if err != nil {
		return false, err
	}
	if st.Pending != nil {
		if launchStateDigest(st) == permit.StateSHA256 {
			return false, nil
		}
		return false, errors.New("Runtime launch transition was replaced")
	}
	if st.Current == nil || st.CurrentActivation != permit.ActivationID || st.Current.Artifact.SHA256 != permit.SHA256 || st.Current.Build != live.Info || st.CurrentLaunchID != permit.LaunchID {
		return false, errors.New("Runtime launch was not selected")
	}
	return true, nil
}
