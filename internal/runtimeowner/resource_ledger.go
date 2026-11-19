package runtimeowner

import (
	"database/sql"
	"errors"
	"os"
	"strings"
)

const (
	ResourceAllocated = "allocated"
	ResourceRetained  = "retained"
	ResourceClosed    = "closed"

	ResourcePhasePreflight = "preflight"
	ResourcePhaseEffects   = "effects"
	ResourcePhaseProcess   = "process"
	ResourcePhaseWorker    = "worker"

	ResourceKindSourceSnapshot = "source_snapshot"
	ResourceKindActivation     = "activation"
	ResourceKindProbe          = "probe"
	ResourceKindProcess        = "process"
	ResourceKindObserver       = "observer"
	ResourceKindJobs           = "jobs"
	ResourceKindRuntime        = "runtime"

	ObserverScopeOwnerProcess = "owner_process"

	ResourceReleaseRequired = "release_required"
	ResourceRetainSelected  = "retain_selected"

	CauseIntentRecorded = "intent_recorded"
	CauseClosed         = "closed"
	CauseOwnerLost      = "owner_lost"
	CauseCleanupUnknown = "cleanup_unknown"

	maxResourceDetail = 256
	maxResourceRows   = 32
)

var ErrResourceConflict = errors.New("resource obligation revision or identity conflict")

type ResourceObligation struct {
	ID                   string `json:"id"`
	Phase                string `json:"phase"`
	Kind                 string `json:"kind"`
	AttemptID            string `json:"attempt_id"`
	Generation           int64  `json:"generation"`
	ActivationID         string `json:"activation_id,omitempty"`
	LaunchID             string `json:"launch_id,omitempty"`
	ImageSHA256          string `json:"image_sha256,omitempty"`
	SourceSnapshotSHA256 string `json:"source_snapshot_sha256,omitempty"`
	ObserverScope        string `json:"observer_scope"`
	Retention            string `json:"retention"`
}

type ResourceClosure struct {
	Cause string `json:"cause"`
}

type ResourceRecord struct {
	ResourceObligation
	OperationID  string `json:"operation_id"`
	NamespaceSHA string `json:"namespace_sha256"`
	Status       string `json:"status"`
	Cause        string `json:"cause"`
	Revision     int64  `json:"revision"`
}

func validResourceIdentity(r ResourceObligation) bool {
	if !idPattern.MatchString(r.ID) || !idPattern.MatchString(r.AttemptID) || r.Generation <= 0 ||
		len(r.Phase) == 0 || len(r.Phase) > maxResourceDetail || len(r.Kind) == 0 || len(r.Kind) > maxResourceDetail ||
		len(r.ObserverScope) == 0 || len(r.ObserverScope) > maxResourceDetail ||
		(r.Retention != ResourceReleaseRequired && r.Retention != ResourceRetainSelected) {
		return false
	}
	if r.ActivationID != "" && !idPattern.MatchString(r.ActivationID) || r.LaunchID != "" && !idPattern.MatchString(r.LaunchID) {
		return false
	}
	if r.ImageSHA256 != "" && !shaPattern.MatchString(r.ImageSHA256) || r.SourceSnapshotSHA256 != "" && !shaPattern.MatchString(r.SourceSnapshotSHA256) {
		return false
	}
	switch r.Phase {
	case ResourcePhasePreflight, ResourcePhaseEffects, ResourcePhaseProcess, ResourcePhaseWorker:
	default:
		return false
	}
	switch r.Kind {
	case ResourceKindSourceSnapshot, ResourceKindActivation, ResourceKindProbe, ResourceKindProcess, ResourceKindObserver, ResourceKindJobs, ResourceKindRuntime:
	default:
		return false
	}
	if r.Retention == ResourceRetainSelected && r.Kind != ResourceKindActivation {
		return false
	}
	return r.ObserverScope == ObserverScopeOwnerProcess
}
func validResourceCause(c string) bool {
	if c == "" || len(c) > maxResourceDetail || strings.TrimSpace(c) != c {
		return false
	}
	switch c {
	case CauseIntentRecorded, CauseClosed, CauseOwnerLost, CauseCleanupUnknown, "selected_receipt":
		return true
	default:
		return false
	}
}

// RecordResourceIntent persists the obligation and an operation revision in one
// SQLite transaction. The caller must invoke it before allocating the resource.
func (t *Transaction) RecordResourceIntent(intent ResourceObligation) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed || t.poisoned || t.record.Outcome != Pending || !validResourceIdentity(intent) || intent.Retention != ResourceReleaseRequired {
		return ErrInvalid
	}
	return t.resourceMutation(func(q resourceQuery) error {
		var n int
		if err := q.QueryRow("SELECT count(*) FROM resource_obligations WHERE operation_id=? AND resource_id=?", t.record.Request.ID, intent.ID).Scan(&n); err != nil {
			return err
		}
		if n != 0 {
			return ErrResourceConflict
		}
		var count int
		if err := q.QueryRow("SELECT count(*) FROM resource_obligations WHERE operation_id=?", t.record.Request.ID).Scan(&count); err != nil {
			return err
		}
		if count >= maxResourceRows {
			return ErrInvalid
		}
		_, err := q.Exec(`INSERT INTO resource_obligations(operation_id,namespace_sha256,resource_id,phase,kind,status,cause,attempt_id,generation,activation_id,launch_id,image_sha256,source_snapshot_sha256,observer_scope,retention,revision) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,1)`, t.record.Request.ID, t.namespace, intent.ID, intent.Phase, intent.Kind, ResourceAllocated, CauseIntentRecorded, intent.AttemptID, intent.Generation, intent.ActivationID, intent.LaunchID, intent.ImageSHA256, intent.SourceSnapshotSHA256, intent.ObserverScope, intent.Retention)
		return err
	})
}

// CloseResource records an owner-observed release. It never infers closure from
// worker exit, lock release, readiness, or a missing observer.
func (t *Transaction) CloseResource(id string, identity ResourceObligation, closure ResourceClosure) error {
	return t.updateResource(id, identity, ResourceClosed, closure)
}
func (t *Transaction) MarkResourceUnknown(id string, identity ResourceObligation, cause string) error {
	return t.updateResource(id, identity, ResourceUnknown, ResourceClosure{Cause: cause})
}
func (t *Transaction) updateResource(id string, identity ResourceObligation, status string, closure ResourceClosure) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.updateResourceLocked(id, identity, status, closure)
}
func (t *Transaction) updateResourceLocked(id string, identity ResourceObligation, status string, closure ResourceClosure) error {
	if t.closed || t.poisoned || (t.record.Outcome != Pending && t.record.Outcome != Uncertain) || id != identity.ID || !validResourceIdentity(identity) || (status != ResourceClosed && status != ResourceUnknown) || !validResourceCause(closure.Cause) {
		return ErrInvalid
	}
	return t.resourceMutation(func(q resourceQuery) error {
		result, err := q.Exec(`UPDATE resource_obligations SET status=?,cause=?,revision=revision+1 WHERE operation_id=? AND namespace_sha256=? AND resource_id=? AND phase=? AND kind=? AND status=? AND attempt_id=? AND generation=? AND activation_id=? AND launch_id=? AND image_sha256=? AND source_snapshot_sha256=? AND observer_scope=? AND retention=?`, status, closure.Cause, t.record.Request.ID, t.namespace, id, identity.Phase, identity.Kind, ResourceAllocated, identity.AttemptID, identity.Generation, identity.ActivationID, identity.LaunchID, identity.ImageSHA256, identity.SourceSnapshotSHA256, identity.ObserverScope, identity.Retention)
		if err != nil {
			return err
		}
		n, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if n != 1 {
			return ErrResourceConflict
		}
		return nil
	})
}

type resourceQuery interface {
	Exec(string, ...any) (sql.Result, error)
	QueryRow(string, ...any) *sql.Row
}

// resourceMutation bumps the operation revision and resource mutation atomically.
func (t *Transaction) resourceMutation(mutate func(resourceQuery) error) error {
	tx, err := t.db.Begin()
	if err != nil {
		return err
	}
	rollback := func(e error) error {
		_ = tx.Rollback()
		if errors.Is(e, ErrResourceConflict) || errors.Is(e, ErrInvalid) {
			return e
		}
		t.poisoned = true
		return errors.Join(ErrUncertain, e)
	}
	if err := mutate(tx); err != nil {
		return rollback(err)
	}
	active := 0
	if t.record.active() {
		active = 1
	}
	result, err := tx.Exec("UPDATE operations SET record=?,revision=? WHERE id=? AND record=? AND active=? AND revision=?", t.last, t.revision+1, t.record.Request.ID, t.last, active, t.revision)
	if err != nil {
		return rollback(err)
	}
	n, err := result.RowsAffected()
	if err != nil || n != 1 {
		if err == nil {
			err = ErrResourceConflict
		}
		return rollback(err)
	}
	if err := tx.Commit(); err != nil {
		t.poisoned = true
		return errors.Join(ErrUncertain, err)
	}
	t.revision++
	return nil
}

func (t *Transaction) hasUnsettledResourceLocked() bool {
	resources, err := readResourceRows(t.db, t.record, t.revision)
	return err != nil || resourcesUnsettled(resources)
}

func (j Journal) Resources(operationID string) ([]ResourceRecord, error) {
	if !idPattern.MatchString(operationID) {
		return nil, ErrInvalid
	}
	db, err := j.open(false)
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrUnknown
	}
	if err != nil {
		return nil, err
	}
	defer db.Close()
	namespace, err := j.NamespaceID()
	if err != nil {
		return nil, err
	}
	record, _, err := query(db, operationID, namespace)
	if err != nil {
		return nil, err
	}
	var revision int64
	if err := db.QueryRow("SELECT revision FROM operations WHERE id=?", operationID).Scan(&revision); err != nil {
		return nil, err
	}
	return readResourceRows(db, record, revision)
}

func readResourceRows(db *sql.DB, record Record, revision int64) ([]ResourceRecord, error) {
	operationID, namespace := record.Request.ID, record.NamespaceSHA256
	rows, err := db.Query(`SELECT operation_id,namespace_sha256,resource_id,phase,kind,status,cause,attempt_id,generation,activation_id,launch_id,image_sha256,source_snapshot_sha256,observer_scope,retention,revision,selection_receipt FROM resource_obligations WHERE operation_id=? ORDER BY resource_id`, operationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]ResourceRecord, 0, maxResourceRows)
	for rows.Next() {
		if len(out) >= maxResourceRows {
			return nil, ErrInvalid
		}
		var r ResourceRecord
		var receipt []byte
		if err := rows.Scan(&r.OperationID, &r.NamespaceSHA, &r.ID, &r.Phase, &r.Kind, &r.Status, &r.Cause, &r.AttemptID, &r.Generation, &r.ActivationID, &r.LaunchID, &r.ImageSHA256, &r.SourceSnapshotSHA256, &r.ObserverScope, &r.Retention, &r.Revision, &receipt); err != nil {
			return nil, ErrInvalid
		}
		if r.Retention == ResourceRetainSelected || r.Status == ResourceRetained {
			if !retainedReceiptValid(receipt, r, record, revision) {
				return nil, ErrInvalid
			}
		} else if len(receipt) != 0 {
			return nil, ErrInvalid
		}
		if !validResourceIdentity(r.ResourceObligation) || !validResourceCause(r.Cause) || (r.Status != ResourceAllocated && r.Status != ResourceRetained && r.Status != ResourceClosed && r.Status != ResourceUnknown) || r.Revision < 1 || r.OperationID != operationID || r.NamespaceSHA != namespace {
			return nil, ErrInvalid
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}
func (j Journal) HasUnsettledResources(operationID string) bool {
	resources, err := j.Resources(operationID)
	return err != nil || resourcesUnsettled(resources)
}
func resourcesUnsettled(resources []ResourceRecord) bool {
	for _, r := range resources {
		if r.Retention == ResourceReleaseRequired && r.Status != ResourceClosed {
			return true
		}
		if r.Retention == ResourceRetainSelected && r.Status != ResourceRetained {
			return true
		}
	}
	return false
}
