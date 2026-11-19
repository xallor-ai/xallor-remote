package runtimeowner

import "github.com/xallor-ai/xallor-remote/internal/release"

// validTransitions validates prefixes as well as terminal receipts. Evidence is
// contiguous from the original baseline; a label cannot invent a prior stop or
// launch. This grants no replay or reconciliation authority.
func (r Record) validTransitions() bool {
	if len(r.Effects) == 0 {
		return true
	}
	if r.Binding == nil {
		return false
	}
	before := r.Binding.Baseline
	phase := "initial"
	var previous Effect
	for index, e := range r.Effects {
		if e.Before != before {
			return false
		}
		if e.State == EffectPending || e.State == EffectUnknown {
			return index == len(r.Effects)-1
		}
		switch e.Purpose {
		case "baseline_prepare":
			if index != 0 || e.Expected == nil || *e.Expected != before {
				return false
			}
		case "requested":
			switch e.Name {
			case "prepare":
				if phase == "initial" && before.Pending.SHA256 != "" {
					return false
				}
				if phase != "initial" && phase != "stopped" {
					return false
				}
				if e.State != EffectConfirmed {
					return false
				}
				if phase == "stopped" {
					if r.Binding.Baseline.Pending.LaunchID != "" && e.Target.LaunchID == r.Binding.Baseline.Pending.LaunchID {
						return false
					}
					phase = "prepared_after_stop"
				} else {
					phase = "prepared"
				}
			case "stop":
				if phase != "prepared" && !(phase == "initial" && before.Pending.SHA256 != "") {
					return false
				}
				if e.State == EffectConfirmed {
					phase = "stopped"
				} else if e.State == EffectNotApplied {
					phase = "stop_refused"
				} else {
					phase = "stop_unknown"
				}
			case "start":
				if phase != "stopped" && phase != "prepared_after_stop" {
					return false
				}
				if phase == "stopped" && r.Binding.Baseline.Pending.SHA256 != "" {
					return false
				}
				if e.State == EffectConfirmed {
					phase = "started"
				} else if e.State == EffectQuiescentFailure {
					phase = "start_failed"
				} else {
					phase = "start_unknown"
				}
			case "commit":
				if phase != "started" || previous.Name != "start" || previous.Target != e.Target || previous.State != EffectConfirmed {
					return false
				}
				phase = "committed"
			case "ready":
				if phase != "committed" || previous.Name != "commit" || previous.State != EffectConfirmed || previous.Target != e.Target {
					return false
				}
				phase = "done"
			case "clear":
				if !r.Binding.ClearOnly || phase != "stopped" {
					return false
				}
				phase = "done"
			default:
				return false
			}
		case "restore":
			switch e.Name {
			case "start":
				if phase != "start_failed" {
					return false
				}
				if e.State == EffectConfirmed {
					phase = "restored_start"
				} else {
					phase = "restore_unknown"
				}
			case "commit":
				if phase != "restored_start" || previous.State != EffectConfirmed || previous.Target != e.Target {
					return false
				}
				phase = "restored_commit"
			case "ready":
				if phase != "restored_commit" || previous.State != EffectConfirmed || previous.Target != e.Target {
					return false
				}
				phase = "done"
			case "clear":
				if phase != "start_failed" || r.Binding.Baseline.Current != (release.SelectionIdentity{}) {
					return false
				}
				phase = "done"
			default:
				return false
			}
		case "cancel":
			if e.Name == "ready" {
				if phase != "cancelled_commit" || previous.State != EffectConfirmed || previous.Target != e.Target {
					return false
				}
				phase = "done"
			} else {
				if phase != "stop_refused" || e.Expected == nil || *e.Expected != r.Binding.Baseline {
					return false
				}
				phase = "cancelled_commit"
			}
		default:
			return false
		}
		if e.Observed != nil {
			before = *e.Observed
		}
		previous = e
	}
	return true
}
