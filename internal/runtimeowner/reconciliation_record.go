package runtimeowner

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
)

const (
	DispositionBusy         = "busy"
	DispositionUnresolved   = "unresolved"
	DispositionResolved     = "resolved"
	RequestNotApplied       = "not_applied"
	RequestRequestedApplied = "requested_applied"
	ResourceSettled         = "settled"
	ResourceUnknown         = "unknown"
)

var (
	ErrReconcileBusy       = errors.New("lifecycle reconciliation is busy")
	ErrReconcileUnresolved = errors.New("lifecycle reconciliation evidence is unresolved")
	ErrRevisionConflict    = errors.New("lifecycle reconciliation revision or digest is stale")
)

// ReconcileRequest is a CAS observation of one original operation. It never
// carries a path, target, effect callback, or replay permission.
type ReconcileRequest struct {
	OperationID      string `json:"operation_id"`
	NamespaceSHA256  string `json:"namespace_sha256"`
	ExpectedRevision int64  `json:"expected_revision"`
	ExpectedDigest   string `json:"expected_record_sha256,omitempty"`
}

type ResourceEvidence struct {
	Kind   string `json:"kind"`
	State  string `json:"state"`
	Detail string `json:"detail,omitempty"`
}

// ReconciliationEvidence is a bounded observation captured while the Runtime
// owner holds lifecycle admission. These values are evidence inputs, not
// authority to clear a record or launch a process.
type ReconciliationEvidence struct {
	AdmissionHeld bool               `json:"admission_held"`
	WorkerActive  bool               `json:"worker_active"`
	Resources     []ResourceEvidence `json:"resources"`
}

type Blocker struct {
	Code   string `json:"code"`
	Detail string `json:"detail,omitempty"`
}

type Resolution struct {
	OriginalOperationID         string    `json:"original_operation_id"`
	OriginalNamespaceSHA        string    `json:"original_namespace_sha256"`
	OriginalRecordSHA256        string    `json:"original_record_sha256"`
	OriginalRevision            int64     `json:"original_revision"`
	EvaluatorCapability         string    `json:"evaluator_capability"`
	Disposition                 string    `json:"disposition"`
	RequestResult               string    `json:"request_result"`
	ResourceState               string    `json:"resource_state"`
	HistoricalReadyAckUncertain bool      `json:"historical_ready_ack_uncertain,omitempty"`
	EvidenceEvaluator           string    `json:"evidence_evaluator,omitempty"`
	Blockers                    []Blocker `json:"blockers,omitempty"`
}

type ReconcileResult struct {
	Original        Record      `json:"original"`
	OriginalDigest  string      `json:"original_record_sha256"`
	Revision        int64       `json:"revision"`
	Resolution      *Resolution `json:"resolution,omitempty"`
	Disposition     string      `json:"disposition"`
	ResourceState   string      `json:"resource_state"`
	EffectiveActive bool        `json:"effective_active"`
	Blockers        []Blocker   `json:"blockers,omitempty"`
}

func digestRecord(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func (r ReconcileRequest) valid() bool {
	return idPattern.MatchString(r.OperationID) && shaPattern.MatchString(r.NamespaceSHA256) && r.ExpectedRevision > 0 && (r.ExpectedDigest == "" || shaPattern.MatchString(r.ExpectedDigest))
}

func (e ReconciliationEvidence) valid() bool {
	if !e.AdmissionHeld || e.WorkerActive || len(e.Resources) != 3 {
		return false
	}
	seen := map[string]bool{}
	for _, r := range e.Resources {
		if seen[r.Kind] || len(r.Kind) > 32 || len(r.State) > 32 || len(r.Detail) > 256 {
			return false
		}
		seen[r.Kind] = true
		if (r.Kind != "jobs" && r.Kind != "runtime" && r.Kind != "observers") || (r.State != "released" && r.State != "absent") {
			return false
		}
	}
	return seen["jobs"] && seen["runtime"] && seen["observers"]
}

func (r Resolution) valid() bool {
	if !idPattern.MatchString(r.OriginalOperationID) || !shaPattern.MatchString(r.OriginalNamespaceSHA) || !shaPattern.MatchString(r.OriginalRecordSHA256) || r.OriginalRevision < 1 || r.EvaluatorCapability == "" || len(r.EvaluatorCapability) > 64 || r.Disposition != DispositionResolved || (r.RequestResult != RequestNotApplied && r.RequestResult != RequestRequestedApplied) || r.ResourceState != ResourceSettled || len(r.Blockers) > 8 || len(r.EvidenceEvaluator) > 64 {
		return false
	}
	if r.RequestResult == RequestRequestedApplied && (!r.HistoricalReadyAckUncertain || r.EvidenceEvaluator == "") {
		return false
	}
	for _, b := range r.Blockers {
		if strings.TrimSpace(b.Code) == "" || len(b.Code) > 64 || len(b.Detail) > 256 {
			return false
		}
	}
	return true
}

func encodeResolution(r Resolution) ([]byte, error) {
	if !r.valid() {
		return nil, ErrInvalid
	}
	raw, err := json.Marshal(r)
	if err != nil || len(raw) > maxRecordBytes {
		return nil, ErrInvalid
	}
	return raw, nil
}
