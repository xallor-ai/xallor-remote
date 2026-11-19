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

const r4EvaluatorCapability = "runtimeowner/reconciliation-r4/v1"

// Reconcile evaluates one original operation under the lifecycle writer lock.
// It never invokes Transaction.Run or any release effect. A successful R4
// decision appends a resolution column while leaving record bytes untouched.
func (j Journal) Reconcile(req ReconcileRequest, evidence ReconciliationEvidence) (ReconcileResult, error) {
	if !req.valid() {
		return ReconcileResult{Disposition: DispositionUnresolved, ResourceState: ResourceUnknown}, ErrInvalid
	}
	ns, err := j.NamespaceID()
	if err != nil || ns != req.NamespaceSHA256 {
		return ReconcileResult{Disposition: DispositionUnresolved, ResourceState: ResourceUnknown}, ErrInvalid
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
	// Reconciliation must not recreate a database removed after the pre-stat.
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
	if len(raw) > maxRecordBytes || len(resolutionRaw) > maxRecordBytes {
		return ReconcileResult{Disposition: DispositionUnresolved, ResourceState: ResourceUnknown}, ErrInvalid
	}
	var original Record
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(&original) != nil || d.Decode(new(any)) != io.EOF || original.ValidateNamespace(req.NamespaceSHA256) != nil || original.Request.ID != req.OperationID || (active != 0 && active != 1) {
		return ReconcileResult{Disposition: DispositionUnresolved, ResourceState: ResourceUnknown}, ErrInvalid
	}
	digest := digestRecord(raw)
	result := ReconcileResult{Original: original, OriginalDigest: digest, Revision: revision, ResourceState: ResourceUnknown, EffectiveActive: active == 1}
	var resolved *Resolution
	if len(resolutionRaw) > 0 {
		var resolution Resolution
		rd := json.NewDecoder(bytes.NewReader(resolutionRaw))
		rd.DisallowUnknownFields()
		if rd.Decode(&resolution) != nil || rd.Decode(new(any)) != io.EOF || !resolution.valid() {
			return result, ErrInvalid
		}
		if resolution.OriginalOperationID != original.Request.ID || resolution.OriginalNamespaceSHA != original.NamespaceSHA256 || resolution.OriginalRecordSHA256 != digest || resolution.OriginalRevision != revision-1 {
			return result, ErrInvalid
		}
		resolved = &resolution
	}
	expectedRevision := revision
	if resolved != nil {
		// Idempotence is bound to the original CAS, not any later observation.
		expectedRevision = resolved.OriginalRevision
	}
	if req.ExpectedDigest != "" && req.ExpectedDigest != digest || req.ExpectedRevision != expectedRevision {
		return result, ErrRevisionConflict
	}
	if j.HasUnsettledResources(req.OperationID) {
		result.Disposition = DispositionUnresolved
		result.Blockers = []Blocker{{Code: "resource_obligations_unsettled"}}
		return result, nil
	}
	if resolved != nil {
		result.Resolution = resolved
		result.Disposition = resolved.Disposition
		result.ResourceState = resolved.ResourceState
		result.EffectiveActive = active == 1
		return result, nil
	}
	blockers := classifyR4(original, evidence)
	if j.HasUnsettledResources(req.OperationID) {
		blockers = append(blockers, Blocker{Code: "resource_debt_unsettled"})
	}
	if len(blockers) != 0 {
		result.Disposition = DispositionUnresolved
		result.Blockers = blockers
		return result, nil
	}
	resolution := Resolution{
		OriginalOperationID:  original.Request.ID,
		OriginalNamespaceSHA: original.NamespaceSHA256,
		OriginalRecordSHA256: digest,
		OriginalRevision:     revision,
		EvaluatorCapability:  r4EvaluatorCapability,
		Disposition:          DispositionResolved,
		RequestResult:        RequestNotApplied,
		ResourceState:        ResourceSettled,
	}
	encoded, err := encodeResolution(resolution)
	if err != nil {
		return result, err
	}
	res, err := db.Exec("UPDATE operations SET resolution=?,revision=? WHERE id=? AND record=? AND resolution IS NULL AND revision=?", encoded, revision+1, req.OperationID, raw, revision)
	if err != nil {
		return result, errors.Join(ErrReconcileUnresolved, err)
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return result, ErrRevisionConflict
	}
	result.Resolution = &resolution
	result.Disposition = DispositionResolved
	result.ResourceState = ResourceSettled
	result.Revision = revision + 1
	result.EffectiveActive = false
	return result, nil
}

// ReconcileR4 is an explicit name for callers that need the tranche boundary.
func (j Journal) ReconcileR4(req ReconcileRequest, evidence ReconciliationEvidence) (ReconcileResult, error) {
	return j.Reconcile(req, evidence)
}
