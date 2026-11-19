package release

import (
	"context"
	"errors"
	"reflect"
	"regexp"

	"github.com/xallor-ai/xallor-remote/internal/buildinfo"
	"github.com/xallor-ai/xallor-remote/internal/protocol"
	"github.com/xallor-ai/xallor-remote/internal/releasetrust"
	"github.com/xallor-ai/xallor-remote/internal/runtimeimage"
)

// RuntimeObservation is an observation, not a reusable launch credential. It
// exposes no executable path and never derives an activation ID from a pathname.
type RuntimeObservation struct {
	State         string `json:"state"`
	Code          string `json:"code,omitempty"`
	Version       string `json:"version"`
	Revision      string `json:"revision"`
	Protocol      int    `json:"protocol"`
	ProfileFormat int    `json:"profile_format"`
	SHA256        string `json:"sha256"`
	PID           int    `json:"pid"`
	ActivationID  string `json:"activation_id,omitempty"`
	LaunchID      string `json:"launch_id,omitempty"`
}

var observedActivationID = regexp.MustCompile(`^[a-f0-9]{32}$`)

// ValidateLaunch compares a ready observation with an independently retained
// launch identity. A state-file observation alone is not process authority:
// the serving daemon must bind this value to its authenticated launch permit.
func (v RuntimeObservation) ValidateLaunch(expected string) error {
	if !observedActivationID.MatchString(expected) || v.State != "ready" || v.Code != "" || v.LaunchID != expected {
		return errors.New("Runtime observation does not match the authenticated launch")
	}
	return nil
}

// ValidateReady checks the owner reply's shape and caller requirements. It is
// not a substitute for the owner's source authentication or process observation.
func (v RuntimeObservation) ValidateReady(requiredProfile, requiredProtocol int) error {
	id := releasetrust.Identity{Version: v.Version, Revision: v.Revision}
	if v.State != "ready" || v.Code != "" || v.Protocol != requiredProtocol || v.ProfileFormat != requiredProfile ||
		v.PID <= 0 || !digestPattern.MatchString(v.SHA256) || !observedActivationID.MatchString(v.ActivationID) ||
		!observedActivationID.MatchString(v.LaunchID) || id.Validate() != nil {
		return errors.New("Runtime owner did not return a complete matching ready identity")
	}
	return nil
}

// ObserveRuntime does not repair state or start/stop anything. Only a stable,
// source-authenticated selection matching this running image can report ready.
func (s Store) ObserveRuntime(ctx context.Context, live buildinfo.Executable) (RuntimeObservation, error) {
	view := RuntimeObservation{State: "unknown", Code: protocol.RuntimeSourceUnverified,
		Version: live.Version, Revision: live.Revision, Protocol: live.Protocol,
		ProfileFormat: live.ProfileFormat, SHA256: live.SHA256, PID: live.PID}
	st, err := s.Read()
	if err != nil {
		return view, err
	}
	if st.Pending != nil || st.PendingActivation != "" {
		view.State, view.Code = "pending", protocol.RuntimePendingRecovery
		return view, errors.New("Runtime selection requires recovery")
	}
	if st.Current == nil || st.CurrentActivation == "" {
		view.State = "unverified"
		return view, errors.New("Runtime has no selected activation")
	}
	if live.OwnerProtocol != buildinfo.RuntimeOwnerProtocol {
		view.Code = protocol.RuntimeProfileMismatch
		return view, errors.New("running Runtime does not support the required owner protocol")
	}
	if live.PID <= 0 || live.Info != st.Current.Build || live.SHA256 != st.Current.Artifact.SHA256 {
		view.Code = protocol.RuntimeStartFailed
		return view, errors.New("running identity differs from selected activation")
	}
	a, verifyErr := s.authenticatedActivation(ctx, st.CurrentActivation, *st.Current)
	// A verification result for a former selection cannot certify its successor.
	// Concurrent owner changes are observed, never retried or repaired here.
	latest, readErr := s.Read()
	if readErr != nil {
		return view, readErr
	}
	if latest.Pending != nil || latest.PendingActivation != "" {
		view.State, view.Code = "pending", protocol.RuntimePendingRecovery
		return view, errors.New("Runtime selection changed to pending during verification")
	}
	if !reflect.DeepEqual(st, latest) {
		view.Code = protocol.RuntimeStartFailed
		return view, errors.New("Runtime selection changed during verification")
	}
	if verifyErr != nil {
		view.State = "unverified"
		return view, verifyErr
	}
	if err := ctx.Err(); err != nil {
		return view, err
	}
	if !runtimeimage.MatchesPath(live.Path, a.Binary) {
		view.Code = protocol.RuntimeStartFailed
		return view, errors.New("running image location differs from selected activation")
	}
	view.State, view.Code, view.ActivationID, view.LaunchID = "ready", "", st.CurrentActivation, st.CurrentLaunchID
	return view, nil
}
