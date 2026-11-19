package runtimeowner

// classifyR4 is deliberately narrow: it recognizes only a terminal original
// admission whose permitted effects prove no dispatch. It does not infer
// anything from empty effects, current disk state, or a stopped worker alone.
func classifyR4(r Record, evidence ReconciliationEvidence) []Blocker {
	block := make([]Blocker, 0, 4)
	if !r.valid() {
		return append(block, Blocker{Code: "invalid_original_record"})
	}
	if r.Outcome != Rejected && r.Outcome != Failed && r.Outcome != Uncertain {
		block = append(block, Blocker{Code: "original_not_terminal"})
	}
	if r.Outcome == Uncertain {
		if len(r.Effects) == 0 {
			block = append(block, Blocker{Code: "uncertain_without_stop_evidence"})
		} else {
			e := r.Effects[len(r.Effects)-1]
			if e.Name != "stop" || e.State != EffectNotApplied || e.StopObserved == nil || e.Stop == nil || !e.StopObserved.NotDispatched(*e.Stop) {
				block = append(block, Blocker{Code: "stop_not_proven_undispatched"})
			}
		}
	}
	if len(r.Effects) == 0 {
		block = append(block, Blocker{Code: "execution_epoch_not_proven_closed"})
	}
	for _, e := range r.Effects {
		if e.State == EffectPending || e.State == EffectUnknown {
			block = append(block, Blocker{Code: "effect_unknown_or_pending"})
			break
		}
		if e.Name == "start" || e.Name == "commit" || e.Name == "ready" {
			block = append(block, Blocker{Code: "external_dispatch_evidence"})
			break
		}
	}
	if !evidence.valid() {
		if evidence.WorkerActive {
			block = append(block, Blocker{Code: "worker_active"})
		} else if !evidence.AdmissionHeld {
			block = append(block, Blocker{Code: "lifecycle_admission_missing"})
		} else {
			block = append(block, Blocker{Code: "resource_closure_incomplete"})
		}
	}
	return block
}

// ClassifyR4 exposes the pure predicate for tests and owner integration.
func ClassifyR4(r Record, evidence ReconciliationEvidence) []Blocker {
	return classifyR4(r, evidence)
}
