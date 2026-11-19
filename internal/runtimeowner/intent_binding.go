package runtimeowner

import (
	"errors"
	"github.com/xallor-ai/xallor-remote/internal/release"
)

// Bind is write-once and precedes every lifecycle effect. Even an identical
// second binding is refused, so a retry cannot silently replace source intent.
func (t *Transaction) Bind(binding release.ResolvedIntent) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed || t.poisoned || t.record.Outcome != Pending || t.record.Binding != nil || len(t.record.Effects) != 0 || !binding.Valid() {
		return errors.Join(release.ErrCleanupUncertain, ErrInvalid)
	}
	next := t.record
	next.Binding = &binding
	if err := t.save(next); err != nil {
		return errors.Join(release.ErrCleanupUncertain, err)
	}
	return nil
}

func sameTarget(a, b release.SelectionIdentity) bool {
	return a.SHA256 == b.SHA256 && a.ActivationID == b.ActivationID
}
func (r Record) validBinding() bool {
	if r.Binding == nil {
		return len(r.Effects) == 0
	}
	b := *r.Binding
	if !b.Valid() || (b.ClearOnly && r.Request.Kind != "rollback") {
		return false
	}
	if r.Request.Kind == "install" && b.Baseline.Pending.SHA256 != "" {
		return false
	}
	if r.Request.Kind == "rollback" && !b.ClearOnly {
		target := b.Baseline.Previous
		if b.Baseline.Pending.SHA256 != "" {
			target = b.Baseline.Current
		}
		if !sameTarget(b.Requested, target) {
			return false
		}
	}
	for _, e := range r.Effects {
		if e.Name == "stop" && e.Stop != nil {
			if e.Stop.Mode == "peer" && e.Stop.Selection != b.Baseline.Current && e.Stop.Selection != b.Baseline.Pending && !(e.Purpose == "requested" && sameTarget(e.Stop.Selection, b.Requested)) {
				return false
			}
			if e.Stop.AbsenceBasis == "fresh" && b.Baseline != release.ObserveSelection(release.State{}) {
				return false
			}
			if e.Stop.AbsenceBasis == "explicit_legacy_recovery" && b.Baseline.Pending.SHA256 == "" {
				return false
			}
		}
		if e.Name == "prepare" && e.Expected != nil && (e.Expected.Current != e.Before.Current || e.Expected.Previous != e.Before.Previous) {
			return false
		}
		if e.Name == "commit" && e.Expected != nil {
			previous := b.Baseline.Previous
			if e.Purpose == "requested" && b.Baseline.Current.SHA256 != b.Requested.SHA256 {
				previous = b.Baseline.Current
				previous.LaunchID = ""
			}
			if e.Expected.Previous != previous {
				return false
			}
		}
		switch e.Purpose {
		case "requested":
			if e.Name == "clear" {
				if !b.ClearOnly {
					return false
				}
			} else if e.Name != "stop" && (b.ClearOnly || !sameTarget(e.Target, b.Requested)) {
				return false
			}
		case "baseline_prepare":
			if e.Name != "commit" && e.Name != "clear" {
				return false
			}
			if e.Name == "commit" && !sameTarget(e.Target, b.Baseline.Current) {
				return false
			}
		case "restore", "cancel":
			if e.Name == "clear" {
				if b.Baseline.Current.SHA256 != "" {
					return false
				}
			} else if !sameTarget(e.Target, b.Baseline.Current) {
				return false
			}
			if e.Name == "commit" {
				if e.Purpose == "cancel" && (e.Expected == nil || *e.Expected != b.Baseline) {
					return false
				}
				if e.Purpose == "restore" && (e.Before.Pending.LaunchID == "" || e.Target.LaunchID != e.Before.Pending.LaunchID) {
					return false
				}
			}
			if e.Name != "start" && e.Name != "commit" && e.Name != "ready" && e.Name != "clear" {
				return false
			}
		default:
			return false
		}
	}
	return true
}
func (r Record) requestedAcknowledged() bool {
	if !r.acknowledged() || r.Binding == nil || r.Binding.ClearOnly {
		return false
	}
	e := r.Effects[len(r.Effects)-1]
	return e.Purpose == "requested" && sameTarget(e.Target, r.Binding.Requested)
}
func (r Record) requestedCleared() bool {
	return r.Binding != nil && r.Binding.ClearOnly && r.cleared() && r.Effects[len(r.Effects)-1].Purpose == "requested"
}
func (r Record) restored() bool {
	if r.cancelledEmpty() {
		return true
	}
	if !r.acknowledged() && !r.cleared() {
		return false
	}
	p := r.Effects[len(r.Effects)-1].Purpose
	return p == "restore" || p == "cancel"
}

func (r Record) cancelledEmpty() bool {
	if r.Binding == nil || r.Binding.Baseline != release.ObserveSelection(release.State{}) || len(r.Effects) < 2 {
		return false
	}
	last := r.Effects[len(r.Effects)-1]
	stop := r.Effects[len(r.Effects)-2]
	return last.Purpose == "cancel" && last.Name == "clear" && last.State == EffectConfirmed && last.Expected != nil && *last.Expected == r.Binding.Baseline && stop.Name == "stop" && stop.State == EffectNotApplied
}
