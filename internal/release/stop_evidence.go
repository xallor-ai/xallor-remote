package release

// StopIntent describes retained live observers, not reusable launch authority.
type StopIntent struct {
	Mode          string            `json:"mode"`
	AttemptID     string            `json:"attempt_id"`
	PeerPID       int               `json:"peer_pid,omitempty"`
	ImageSHA256   string            `json:"image_sha256,omitempty"`
	Selection     SelectionIdentity `json:"selection"`
	IdentityScope string            `json:"identity_scope,omitempty"`
	AbsenceBasis  string            `json:"absence_basis,omitempty"`
}

type StopObservation struct {
	AttemptID       string `json:"attempt_id"`
	Dispatch        string `json:"dispatch"`
	Process         string `json:"process"`
	RuntimeResource string `json:"runtime_resource"`
	Jobs            string `json:"jobs"`
	Observers       string `json:"observers"`
	Result          string `json:"result"`
}

func (i StopIntent) Valid() bool {
	if !observedActivationID.MatchString(i.AttemptID) {
		return false
	}
	switch i.Mode {
	case "peer":
		return i.PeerPID > 0 && evidenceSHA.MatchString(i.ImageSHA256) && i.AbsenceBasis == "" && ((i.IdentityScope == "process_image_and_launch" && i.Selection.Complete() && i.Selection.SHA256 == i.ImageSHA256) || (i.IdentityScope == "process_image" && i.Selection == (SelectionIdentity{})))
	case "no_peer":
		return i.PeerPID == 0 && i.ImageSHA256 == "" && i.Selection == (SelectionIdentity{}) && i.IdentityScope == "" && (i.AbsenceBasis == "fresh" || i.AbsenceBasis == "explicit_legacy_recovery" || i.AbsenceBasis == "public_observation")
	}
	return false
}

func (o StopObservation) Valid(i StopIntent) bool {
	if !i.Valid() || o.AttemptID != i.AttemptID {
		return false
	}
	if o.Dispatch != "not_sent" && o.Dispatch != "may_have_been_sent" && o.Dispatch != "acknowledged" {
		return false
	}
	if o.Process != "exit_observed" && o.Process != "still_running_or_unknown" && o.Process != "not_observed" {
		return false
	}
	if o.RuntimeResource != "released_under_lock" && o.RuntimeResource != "held_by_recovery" && o.RuntimeResource != "unknown" && o.RuntimeResource != "absent_profile" {
		return false
	}
	if o.Jobs != "absent" && o.Jobs != "empty" && o.Jobs != "reserved_only" && o.Jobs != "unreleased_or_unknown" {
		return false
	}
	if o.Observers != "released" && o.Observers != "unknown" {
		return false
	}
	if i.Mode == "no_peer" && (o.Dispatch != "not_sent" || o.Process != "not_observed") {
		return false
	}
	return o.Result == "released_peer" || o.Result == "quiescent_no_peer" || o.Result == "not_dispatched" || o.Result == "unknown"
}

func (o StopObservation) NotDispatched(i StopIntent) bool {
	return o.Valid(i) && o.Result == "not_dispatched" && o.Dispatch == "not_sent" && o.Observers == "released"
}

func (o StopObservation) Confirms(i StopIntent) bool {
	if !o.Valid(i) || o.Observers != "released" || o.RuntimeResource == "unknown" || o.Jobs == "unreleased_or_unknown" {
		return false
	}
	if o.RuntimeResource == "absent_profile" && (i.AbsenceBasis != "public_observation" || o.Jobs != "absent") {
		return false
	}
	if i.Mode == "peer" {
		return o.Result == "released_peer" && o.Process == "exit_observed" && o.Dispatch != "may_have_been_sent"
	}
	return o.Result == "quiescent_no_peer" && (i.AbsenceBasis != "explicit_legacy_recovery" || o.RuntimeResource == "held_by_recovery")
}
