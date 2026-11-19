package release

import "regexp"

// SelectionEvidence is a bounded observation of the exact decoded State. The
// digest includes manifests; references expose identities without executable paths.
// None of these values authorizes execution or proves process/resource release.
type SelectionEvidence struct {
	StateSHA256 string            `json:"state_sha256"`
	Current     SelectionIdentity `json:"current"`
	Previous    SelectionIdentity `json:"previous"`
	Pending     SelectionIdentity `json:"pending"`
}

type SelectionIdentity struct {
	SHA256       string `json:"sha256,omitempty"`
	ActivationID string `json:"activation_id,omitempty"`
	LaunchID     string `json:"launch_id,omitempty"`
}

// EffectIntent is captured before dispatch. Expected is populated only for
// selection writes; it must never be substituted for a subsequent observation.
type EffectIntent struct {
	Purpose      string             `json:"purpose"`
	Stop         *StopIntent        `json:"stop,omitempty"`
	Name         string             `json:"name"`
	TargetSHA256 string             `json:"target_sha256,omitempty"`
	Before       SelectionEvidence  `json:"before"`
	Expected     *SelectionEvidence `json:"expected,omitempty"`
	Target       SelectionIdentity  `json:"target"`
}

var evidenceSHA = regexp.MustCompile(`^[a-f0-9]{64}$`)

func selectionIdentity(m *Manifest, activation, launch string) SelectionIdentity {
	sha := ""
	if m != nil {
		sha = m.Artifact.SHA256
	}
	return SelectionIdentity{sha, activation, launch}
}

func ObserveSelection(st State) SelectionEvidence {
	return SelectionEvidence{launchStateDigest(st), selectionIdentity(st.Current, st.CurrentActivation, st.CurrentLaunchID), selectionIdentity(st.Previous, st.PreviousActivation, ""), selectionIdentity(st.Pending, st.PendingActivation, st.PendingLaunchID)}
}

func (i SelectionIdentity) valid() bool {
	if i.SHA256 == "" {
		return i.ActivationID == "" && i.LaunchID == ""
	}
	return evidenceSHA.MatchString(i.SHA256) && (i.ActivationID == "" || observedActivationID.MatchString(i.ActivationID)) && (i.LaunchID == "" || (i.ActivationID != "" && observedActivationID.MatchString(i.LaunchID)))
}

func (i SelectionIdentity) Complete() bool {
	return i.valid() && i.SHA256 != "" && i.ActivationID != "" && i.LaunchID != ""
}

func (s SelectionEvidence) Valid() bool {
	if !evidenceSHA.MatchString(s.StateSHA256) || !s.Current.valid() || !s.Previous.valid() || !s.Pending.valid() || s.Previous.LaunchID != "" {
		return false
	}
	if s.Current.SHA256 == "" && s.Previous.SHA256 == "" && s.Pending.SHA256 == "" {
		return s == ObserveSelection(State{})
	}
	return true
}

func (e EffectIntent) Valid() bool {
	if e.Purpose != "requested" && e.Purpose != "baseline_prepare" && e.Purpose != "restore" && e.Purpose != "cancel" {
		return false
	}
	if (e.Name == "stop") != (e.Stop != nil) {
		return false
	}
	if e.Stop != nil && !e.Stop.Valid() {
		return false
	}
	if !e.Before.Valid() || !e.Target.valid() || e.TargetSHA256 != e.Target.SHA256 {
		return false
	}
	write := e.Name == "prepare" || e.Name == "commit" || e.Name == "clear"
	if write != (e.Expected != nil) || (write && !e.Expected.Valid()) {
		return false
	}
	switch e.Name {
	case "stop":
		if e.Stop.AbsenceBasis == "public_observation" {
			return false
		}
		if e.Stop.Mode == "peer" && (e.Stop.IdentityScope != "process_image_and_launch" || (e.Stop.Selection != e.Before.Current && e.Stop.Selection != e.Before.Pending)) {
			return false
		}
		if e.Stop.AbsenceBasis == "fresh" && (e.Before.Current.SHA256 != "" || e.Before.Previous.SHA256 != "") {
			return false
		}
		if e.Stop.AbsenceBasis == "explicit_legacy_recovery" && e.Before.Pending.SHA256 == "" {
			return false
		}
		return e.Target == (SelectionIdentity{})
	case "start":
		if !e.Target.Complete() || e.Before.Pending.LaunchID != e.Target.LaunchID {
			return false
		}
		return e.Target == e.Before.Pending || (e.Target.SHA256 == e.Before.Current.SHA256 && e.Target.ActivationID == e.Before.Current.ActivationID)
	case "ready":
		return e.Target.Complete() && e.Target == e.Before.Current && e.Before.Pending == (SelectionIdentity{})
	case "prepare":
		return e.Target.Complete() && e.Target == e.Expected.Pending
	case "commit":
		return e.Target.Complete() && e.Target == e.Expected.Current && e.Expected.Pending == (SelectionIdentity{})
	case "clear":
		return e.Target == (SelectionIdentity{}) && *e.Expected == ObserveSelection(State{})
	}
	return false
}

func (e EffectIntent) MatchesObserved(observed SelectionEvidence) bool {
	if !observed.Valid() {
		return false
	}
	if e.Expected != nil {
		return observed == *e.Expected
	}
	return observed == e.Before
}
