package runtimeowner

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	"github.com/xallor-ai/xallor-remote/internal/release"
)

const r1EvaluatorCapability = "runtimeowner/reconciliation-r1/v1"

// R1CurrentEvidence is an owner-only, independently captured present-state
// observation. It is evidence for classification, never a launch capability.
type R1CurrentEvidence struct {
	OwnerAuthenticated   bool                      `json:"owner_authenticated"`
	PeerPID              int64                     `json:"peer_pid"`
	ActivationID         string                    `json:"activation_id"`
	LaunchID             string                    `json:"launch_id"`
	LoadedImageSHA256    string                    `json:"loaded_image_sha256"`
	SourceManifestSHA256 string                    `json:"source_manifest_sha256"`
	Selection            release.SelectionEvidence `json:"selection"`
	ReadyStable          bool                      `json:"ready_stable"`
}

// R1Evidence is intentionally not accepted by IPC or CLI. A Runtime owner may
// later populate it from native process/source observers; this tranche only
// provides the strict pure classifier.
type R1Evidence struct {
	NamespaceSHA256  string             `json:"namespace_sha256"`
	RecordSHA256     string             `json:"record_sha256"`
	AdmissionHeld    bool               `json:"admission_held"`
	WorkerActive     bool               `json:"worker_active"`
	ReconcilerActive bool               `json:"reconciler_active"`
	Current          R1CurrentEvidence  `json:"current"`
	Resources        []ResourceEvidence `json:"resources"`
}

func r1RecordDigest(r Record) string {
	raw, err := json.Marshal(r)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func r1ResourcesValid(resources []ResourceEvidence) bool {
	if len(resources) != 5 {
		return false
	}
	required := map[string]bool{"jobs": false, "runtime": false, "observers": false, "source": false, "probe": false}
	for _, resource := range resources {
		if _, ok := required[resource.Kind]; !ok || required[resource.Kind] || len(resource.Kind) > 32 || len(resource.State) > 32 || len(resource.Detail) > 256 {
			return false
		}
		if resource.State != ResourceSettled && resource.State != "released" && resource.State != "absent" {
			return false
		}
		required[resource.Kind] = true
	}
	for _, present := range required {
		if !present {
			return false
		}
	}
	return true
}

func classifyR1(r Record, evidence R1Evidence) []Blocker {
	blockers := make([]Blocker, 0, 8)
	if !r.valid() {
		return append(blockers, Blocker{Code: "invalid_original_record"})
	}
	if r.Outcome != Uncertain {
		blockers = append(blockers, Blocker{Code: "original_not_ack_uncertain"})
	}
	if r.Binding == nil || r.Binding.ClearOnly {
		blockers = append(blockers, Blocker{Code: "requested_binding_missing"})
	}
	if evidence.NamespaceSHA256 != r.NamespaceSHA256 || !shaPattern.MatchString(evidence.NamespaceSHA256) {
		blockers = append(blockers, Blocker{Code: "namespace_mismatch"})
	}
	if !shaPattern.MatchString(evidence.RecordSHA256) || evidence.RecordSHA256 != r1RecordDigest(r) {
		blockers = append(blockers, Blocker{Code: "record_digest_stale"})
	}
	if !evidence.AdmissionHeld {
		blockers = append(blockers, Blocker{Code: "lifecycle_admission_missing"})
	}
	if evidence.WorkerActive {
		blockers = append(blockers, Blocker{Code: "worker_active"})
	}
	if evidence.ReconcilerActive {
		blockers = append(blockers, Blocker{Code: "reconciler_active"})
	}
	if !r1ResourcesValid(evidence.Resources) {
		blockers = append(blockers, Blocker{Code: "resource_debt_unsettled"})
	}
	if r.Binding == nil {
		return blockers
	}
	requestedCommit := Effect{}
	foundCommit := false
	readySeen := false
	for _, effect := range r.Effects {
		if effect.Name == "commit" && effect.Purpose == "requested" && effect.State == EffectConfirmed && effect.Target.Complete() && effect.Target.SHA256 == r.Binding.Requested.SHA256 && effect.Target.ActivationID == r.Binding.Requested.ActivationID {
			requestedCommit = effect
			foundCommit = true
		}
		if effect.Name == "ready" {
			readySeen = true
			if effect.State == EffectConfirmed || effect.Purpose != "requested" {
				blockers = append(blockers, Blocker{Code: "ready_ack_not_historically_uncertain"})
			}
		}
		if effect.Purpose == "restore" || effect.Purpose == "cancel" {
			blockers = append(blockers, Blocker{Code: "baseline_restore_evidence"})
		}
	}
	if !foundCommit {
		blockers = append(blockers, Blocker{Code: "requested_commit_missing"})
	}
	if readySeen {
		last := r.Effects[len(r.Effects)-1]
		if last.Name != "ready" || (last.State != EffectUnknown && last.State != EffectPending) {
			blockers = append(blockers, Blocker{Code: "ready_ack_not_missing_or_unknown"})
		}
	}
	if foundCommit {
		target := requestedCommit.Target
		current := evidence.Current
		if !current.OwnerAuthenticated || current.PeerPID <= 0 || len(current.ActivationID) > 32 || len(current.LaunchID) > 32 || current.ActivationID != target.ActivationID || current.LaunchID != target.LaunchID {
			blockers = append(blockers, Blocker{Code: "owner_process_identity_mismatch"})
		}
		if current.LoadedImageSHA256 != target.SHA256 || !shaPattern.MatchString(current.LoadedImageSHA256) {
			blockers = append(blockers, Blocker{Code: "loaded_image_mismatch"})
		}
		if current.SourceManifestSHA256 != r.Binding.ManifestSHA256 || !shaPattern.MatchString(current.SourceManifestSHA256) {
			blockers = append(blockers, Blocker{Code: "source_proof_mismatch"})
		}
		if !current.ReadyStable || !current.Selection.Valid() || requestedCommit.Observed == nil || current.Selection != *requestedCommit.Observed || current.Selection.Current != target || current.Selection.Pending != (release.SelectionIdentity{}) {
			blockers = append(blockers, Blocker{Code: "current_selection_not_stable_requested"})
		}
	}
	return blockers
}

// ClassifyR1 exposes the pure present-state predicate. It has no public
// authority and never mutates a journal or starts/stops/replays a process.
func ClassifyR1(r Record, evidence R1Evidence) []Blocker {
	return classifyR1(r, evidence)
}

// R1EvaluatorCapability is retained for a future owner-only resolution writer.
func R1EvaluatorCapability() string { return r1EvaluatorCapability }
