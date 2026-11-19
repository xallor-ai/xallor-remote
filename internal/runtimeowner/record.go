// Package runtimeowner owns local lifecycle transaction admission. Its records
// describe observations, never permission to launch or resume a process.
package runtimeowner

import (
	"errors"
	"github.com/xallor-ai/xallor-remote/internal/release"
	"regexp"
)

var (
	ErrUnknown   = errors.New("lifecycle request is not recorded")
	ErrRecorded  = errors.New("lifecycle request already recorded; query only")
	ErrConflict  = errors.New("lifecycle request ID has a different intent")
	ErrPending   = errors.New("unfinished lifecycle transaction requires explicit reconciliation")
	ErrUncertain = errors.New("lifecycle effect is unconfirmed; automatic replay refused")
	ErrInvalid   = errors.New("invalid lifecycle record")
	idPattern    = regexp.MustCompile(`^[a-f0-9]{32}$`)
	shaPattern   = regexp.MustCompile(`^[a-f0-9]{64}$`)
)

type Outcome string

const (
	Pending   Outcome = "pending"
	Succeeded Outcome = "succeeded"
	Rejected  Outcome = "rejected"
	Failed    Outcome = "failed"
	Uncertain Outcome = "uncertain"
)

type EffectState string

const (
	EffectPending          EffectState = "pending"
	EffectConfirmed        EffectState = "confirmed"
	EffectNotApplied       EffectState = "not_applied"
	EffectUnknown          EffectState = "unknown"
	EffectQuiescentFailure EffectState = "quiescent_failure"
)

type Request struct {
	ID           string `json:"id"`
	Kind         string `json:"kind"`
	IntentSHA256 string `json:"intent_sha256"`
}
type Effect struct {
	release.EffectIntent
	StopObserved *release.StopObservation   `json:"stop_observed,omitempty"`
	State        EffectState                `json:"state"`
	Observed     *release.SelectionEvidence `json:"observed,omitempty"`
}
type Record struct {
	Binding         *release.ResolvedIntent `json:"binding,omitempty"`
	Request         Request                 `json:"request"`
	NamespaceSHA256 string                  `json:"namespace_sha256"`
	Outcome         Outcome                 `json:"outcome"`
	Effects         []Effect                `json:"effects"`
}

// ValidateNamespace checks correlation with the caller's retained profile
// context. A valid operation ID/intent from another profile is insufficient.
func (r Record) ValidateNamespace(expected string) error {
	if !r.valid() || !shaPattern.MatchString(expected) || r.NamespaceSHA256 != expected {
		return ErrInvalid
	}
	return nil
}

// ValidateFor checks received observation data, not an execution capability.
func (r Record) ValidateFor(request Request) error {
	if !r.valid() {
		return ErrInvalid
	}
	if r.Request != request {
		return ErrConflict
	}
	return nil
}

func (r Record) ValidateID(id string) error {
	if !r.valid() || r.Request.ID != id {
		return ErrInvalid
	}
	return nil
}

func (r Request) valid() bool {
	return idPattern.MatchString(r.ID) && shaPattern.MatchString(r.IntentSHA256) && (r.Kind == "install" || r.Kind == "rollback")
}
func (e Effect) valid() bool {
	if !e.EffectIntent.Valid() {
		return false
	}
	if e.Name != "stop" && e.StopObserved != nil {
		return false
	}
	if e.StopObserved != nil && (e.Stop == nil || !e.StopObserved.Valid(*e.Stop)) {
		return false
	}
	if e.Name == "stop" && e.State != EffectPending && e.State != EffectUnknown {
		if e.StopObserved == nil {
			return false
		}
		if e.State == EffectConfirmed {
			if !e.StopObserved.Confirms(*e.Stop) {
				return false
			}
		} else if e.State != EffectNotApplied || !e.StopObserved.NotDispatched(*e.Stop) {
			return false
		}
	}
	if e.State == EffectPending {
		if e.StopObserved != nil {
			return false
		}
		if e.Observed != nil {
			return false
		}
	} else if e.State != EffectUnknown {
		if e.Observed == nil || !e.MatchesObserved(*e.Observed) {
			return false
		}
	} else if e.Observed != nil && !e.Observed.Valid() {
		return false
	}
	if e.State == EffectNotApplied && e.Name != "stop" {
		return false
	}
	if e.Name != "stop" && e.Name != "start" && e.Name != "commit" && e.Name != "ready" && e.Name != "prepare" && e.Name != "clear" {
		return false
	}
	if e.Name == "clear" && e.TargetSHA256 != "" {
		return false
	}
	if ((e.Name != "stop" && e.Name != "clear") || e.TargetSHA256 != "") && !shaPattern.MatchString(e.TargetSHA256) {
		return false
	}
	return e.State == EffectPending || e.State == EffectConfirmed || e.State == EffectNotApplied || e.State == EffectUnknown || (e.Name == "start" && e.State == EffectQuiescentFailure)
}
func (r Record) active() bool { return r.Outcome == Pending || r.Outcome == Uncertain }
func (r Record) unresolved() bool {
	for _, e := range r.Effects {
		if e.State == EffectPending || e.State == EffectUnknown {
			return true
		}
	}
	return false
}
func (r Record) acknowledged() bool {
	n := len(r.Effects)
	if n < 2 {
		return false
	}
	a, b := r.Effects[n-2], r.Effects[n-1]
	return a.Name == "commit" && b.Name == "ready" && a.State == EffectConfirmed && b.State == EffectConfirmed && a.Purpose == b.Purpose && a.Target == b.Target && a.Observed != nil && *a.Observed == b.Before
}

func (r Record) cleared() bool {
	n := len(r.Effects)
	if n == 0 || r.Effects[n-1].Name != "clear" || r.Effects[n-1].State != EffectConfirmed {
		return false
	}
	for i := n - 2; i >= 0; i-- {
		e := r.Effects[i]
		if e.Name == "stop" {
			return e.State == EffectConfirmed
		}
		if e.Name == "start" {
			return e.State == EffectQuiescentFailure
		}
	}
	return false
}
func (r Record) valid() bool {
	if !r.validBinding() || !r.validTransitions() || !r.Request.valid() || !shaPattern.MatchString(r.NamespaceSHA256) || len(r.Effects) > 32 {
		return false
	}
	for i, e := range r.Effects {
		if !e.valid() || ((e.State == EffectPending || e.State == EffectUnknown) && i != len(r.Effects)-1) {
			return false
		}
	}
	switch r.Outcome {
	case Pending, Uncertain:
		return true
	case Rejected:
		return len(r.Effects) == 0
	case Succeeded:
		return !r.unresolved() && (r.requestedAcknowledged() || (r.Request.Kind == "rollback" && r.requestedCleared()))
	case Failed:
		if r.unresolved() {
			return false
		}
		if r.restored() {
			return true
		} // Independently confirmed restoration.
		for _, e := range r.Effects {
			if e.State != EffectNotApplied {
				return false
			}
		}
		return true
	}
	return false
}
