package runtimeowner

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"

	"github.com/xallor-ai/xallor-remote/internal/filelock"
)

// ReconcileR1 appends an owner-only requested-applied resolution. It never
// rewrites the original record and never invokes a lifecycle effect.
func (j Journal) ReconcileR1(req ReconcileRequest, evidence R1Evidence) (ReconcileResult, error) {
	if !req.valid() {
		return ReconcileResult{Disposition: DispositionUnresolved, ResourceState: ResourceUnknown}, ErrInvalid
	}
	ns, err := j.NamespaceID()
	if err != nil || ns != req.NamespaceSHA256 {
		return ReconcileResult{Disposition: DispositionUnresolved, ResourceState: ResourceUnknown}, ErrInvalid
	}
	// Reconciliation is observational and must not initialize a missing profile.
	if _, statErr := os.Stat(filepath.Join(j.Dir, "operations.db")); os.IsNotExist(statErr) {
		return ReconcileResult{Disposition: DispositionUnresolved, ResourceState: ResourceUnknown}, ErrUnknown
	} else if statErr != nil {
		return ReconcileResult{Disposition: DispositionUnresolved, ResourceState: ResourceUnknown}, statErr
	}
	if _, err := os.Stat(filepath.Join(j.Dir, "operations.db")); os.IsNotExist(err) {
		return ReconcileResult{Disposition: DispositionUnresolved, ResourceState: ResourceUnknown}, ErrUnknown
	}
	unlock, err := filelock.Acquire(filepath.Join(j.Dir, "lifecycle.lock"))
	if err != nil {
		return ReconcileResult{Disposition: DispositionBusy, ResourceState: ResourceUnknown}, errors.Join(ErrReconcileBusy, err)
	}
	defer unlock()
	evidence.AdmissionHeld = true
	// Open existing DB writable without O_CREATE; the pre-stat plus this mode
	// prevents reconciliation from creating a missing profile/database.
	db, err := j.openExistingWritable()
	if os.IsNotExist(err) {
		return ReconcileResult{Disposition: DispositionUnresolved, ResourceState: ResourceUnknown}, ErrUnknown
	}
	if err != nil {
		return ReconcileResult{Disposition: DispositionUnresolved, ResourceState: ResourceUnknown}, err
	}
	defer db.Close()

	var raw, resolutionRaw []byte
	var revision int64
	var active int
	err = db.QueryRow("SELECT record,resolution,revision,active FROM operations WHERE id=?", req.OperationID).Scan(&raw, &resolutionRaw, &revision, &active)
	if errors.Is(err, sql.ErrNoRows) {
		return ReconcileResult{Disposition: DispositionUnresolved, ResourceState: ResourceUnknown}, ErrUnknown
	}
	if err != nil {
		return ReconcileResult{Disposition: DispositionUnresolved, ResourceState: ResourceUnknown}, err
	}
	if len(raw) == 0 || len(raw) > maxRecordBytes || len(resolutionRaw) > maxRecordBytes || revision < 1 || (active != 0 && active != 1) {
		return ReconcileResult{Disposition: DispositionUnresolved, ResourceState: ResourceUnknown}, ErrInvalid
	}
	var original Record
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&original) != nil || decoder.Decode(new(any)) != io.EOF || original.ValidateNamespace(ns) != nil || original.Request.ID != req.OperationID {
		return ReconcileResult{Disposition: DispositionUnresolved, ResourceState: ResourceUnknown}, ErrInvalid
	}
	digest := digestRecord(raw)
	result := ReconcileResult{Original: original, OriginalDigest: digest, Revision: revision, ResourceState: ResourceUnknown, EffectiveActive: active == 1}
	if j.HasUnsettledResources(req.OperationID) {
		result.Disposition = DispositionUnresolved
		result.Blockers = []Blocker{{Code: "resource_obligations_unsettled"}}
		return result, nil
	}

	// A resolution is idempotent only for the same original binding and CAS
	// observation. A different resolution or stale digest must be refused.
	if len(resolutionRaw) > 0 {
		var resolution Resolution
		rd := json.NewDecoder(bytes.NewReader(resolutionRaw))
		rd.DisallowUnknownFields()
		if rd.Decode(&resolution) != nil || rd.Decode(new(any)) != io.EOF || !resolution.valid() || resolution.EvaluatorCapability != r1EvaluatorCapability || resolution.RequestResult != RequestRequestedApplied || !resolution.HistoricalReadyAckUncertain || resolution.EvidenceEvaluator != r1EvaluatorCapability ||
			resolution.OriginalOperationID != original.Request.ID ||
			resolution.OriginalNamespaceSHA != original.NamespaceSHA256 ||
			resolution.OriginalRecordSHA256 != digest ||
			resolution.OriginalRevision != revision-1 {
			return result, ErrInvalid
		}
		if req.ExpectedDigest != "" && req.ExpectedDigest != digest || req.ExpectedRevision != resolution.OriginalRevision {
			return result, ErrRevisionConflict
		}
		result.Resolution = &resolution
		result.Disposition = resolution.Disposition
		result.ResourceState = resolution.ResourceState
		return result, nil
	}
	if req.ExpectedDigest != "" && req.ExpectedDigest != digest || req.ExpectedRevision != revision {
		return result, ErrRevisionConflict
	}
	blockers := classifyR1(original, evidence)
	if j.HasUnsettledResources(req.OperationID) {
		blockers = append(blockers, Blocker{Code: "resource_debt_unsettled"})
	}
	if len(blockers) != 0 {
		result.Disposition = DispositionUnresolved
		result.Blockers = blockers
		return result, nil
	}
	resolution := Resolution{
		OriginalOperationID:         original.Request.ID,
		OriginalNamespaceSHA:        original.NamespaceSHA256,
		OriginalRecordSHA256:        digest,
		OriginalRevision:            revision,
		EvaluatorCapability:         r1EvaluatorCapability,
		Disposition:                 DispositionResolved,
		RequestResult:               RequestRequestedApplied,
		ResourceState:               ResourceSettled,
		HistoricalReadyAckUncertain: true,
		EvidenceEvaluator:           r1EvaluatorCapability,
	}
	encoded, err := encodeResolution(resolution)
	if err != nil {
		return result, err
	}
	updated, err := db.Exec("UPDATE operations SET resolution=?,revision=? WHERE id=? AND record=? AND resolution IS NULL AND revision=?", encoded, revision+1, req.OperationID, raw, revision)
	if err != nil {
		return result, errors.Join(ErrReconcileUnresolved, err)
	}
	if n, _ := updated.RowsAffected(); n != 1 {
		return result, ErrRevisionConflict
	}
	result.Resolution = &resolution
	result.Disposition = DispositionResolved
	result.ResourceState = ResourceSettled
	result.Revision = revision + 1
	result.EffectiveActive = false
	return result, nil
}
