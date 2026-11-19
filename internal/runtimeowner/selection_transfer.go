package runtimeowner

import (
	"bytes"
	"encoding/json"
	"github.com/xallor-ai/xallor-remote/internal/release"
	"io"
)

// SelectionOwnerEvidence is captured by the authenticated Runtime owner after
// the requested commit and ready acknowledgements. It is evidence, never a
// caller supplied launch capability.
type SelectionOwnerEvidence struct {
	Current              R1CurrentEvidence
	SourceSnapshotSHA256 string
}

type selectionReceiptData struct {
	OperationID          string                    `json:"operation_id"`
	NamespaceSHA256      string                    `json:"namespace_sha256"`
	ResourceID           string                    `json:"resource_id"`
	Resource             ResourceObligation        `json:"resource"`
	ResourceRevision     int64                     `json:"resource_revision"`
	RecordSHA256         string                    `json:"record_sha256"`
	RecordRevision       int64                     `json:"record_revision"`
	ActivationID         string                    `json:"activation_id"`
	LaunchID             string                    `json:"launch_id"`
	ImageSHA256          string                    `json:"image_sha256"`
	SourceSnapshotSHA256 string                    `json:"source_snapshot_sha256"`
	SelectionSHA256      string                    `json:"selection_sha256"`
	Selection            release.SelectionEvidence `json:"selection"`
	ManifestSHA256       string                    `json:"manifest_sha256"`
	Purpose              string                    `json:"purpose"`
	PeerPID              int64                     `json:"peer_pid"`
}

// SelectionReceipt is an owner minted, transaction bound receipt. The owner
// token is intentionally unexported, so a decoded or caller-created value has
// no authority to mutate the journal.
type SelectionReceipt struct {
	data  selectionReceiptData
	owner *Transaction
}

func (t *Transaction) SelectionReceipt(resource ResourceObligation, expectedRevision int64, evidence SelectionOwnerEvidence) (SelectionReceipt, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed || t.poisoned || t.record.Outcome != Pending || expectedRevision < 1 || !validResourceIdentity(resource) || resource.Kind != ResourceKindActivation || resource.Retention != ResourceReleaseRequired || !validSelectionOwnerEvidence(evidence) {
		return SelectionReceipt{}, ErrInvalid
	}
	if !requestedSelectionAcknowledged(t.record, evidence.Current) || resource.ActivationID != evidence.Current.ActivationID || resource.LaunchID != evidence.Current.LaunchID || resource.ImageSHA256 != evidence.Current.LoadedImageSHA256 || resource.SourceSnapshotSHA256 != evidence.SourceSnapshotSHA256 {
		return SelectionReceipt{}, ErrInvalid
	}
	var row ResourceRecord
	err := t.db.QueryRow(`SELECT operation_id,namespace_sha256,resource_id,phase,kind,status,cause,attempt_id,generation,activation_id,launch_id,image_sha256,source_snapshot_sha256,observer_scope,retention,revision FROM resource_obligations WHERE operation_id=? AND namespace_sha256=? AND resource_id=?`, t.record.Request.ID, t.namespace, resource.ID).Scan(&row.OperationID, &row.NamespaceSHA, &row.ID, &row.Phase, &row.Kind, &row.Status, &row.Cause, &row.AttemptID, &row.Generation, &row.ActivationID, &row.LaunchID, &row.ImageSHA256, &row.SourceSnapshotSHA256, &row.ObserverScope, &row.Retention, &row.Revision)
	if err != nil || row.Status != ResourceAllocated || row.Revision != expectedRevision || row.ResourceObligation != resource {
		return SelectionReceipt{}, ErrResourceConflict
	}
	return SelectionReceipt{owner: t, data: selectionReceiptData{OperationID: t.record.Request.ID, NamespaceSHA256: t.namespace, ResourceID: resource.ID, Resource: resource, ResourceRevision: row.Revision, RecordSHA256: r1RecordDigest(t.record), RecordRevision: t.revision, ActivationID: resource.ActivationID, LaunchID: resource.LaunchID, ImageSHA256: resource.ImageSHA256, SourceSnapshotSHA256: resource.SourceSnapshotSHA256, SelectionSHA256: evidence.Current.Selection.StateSHA256, Selection: evidence.Current.Selection, ManifestSHA256: evidence.Current.SourceManifestSHA256, Purpose: "requested", PeerPID: evidence.Current.PeerPID}}, nil
}

func validSelectionOwnerEvidence(e SelectionOwnerEvidence) bool {
	return e.Current.OwnerAuthenticated && e.Current.PeerPID > 0 && e.Current.ReadyStable && e.Current.Selection.Valid() && shaPattern.MatchString(e.SourceSnapshotSHA256) && shaPattern.MatchString(e.Current.SourceManifestSHA256) && shaPattern.MatchString(e.Current.LoadedImageSHA256)
}

func requestedSelectionAcknowledged(r Record, current R1CurrentEvidence) bool {
	if !r.valid() || r.Binding == nil || r.Binding.ClearOnly || current.Selection.Pending != (release.SelectionIdentity{}) || !sameTarget(current.Selection.Current, r.Binding.Requested) || current.LoadedImageSHA256 != r.Binding.Requested.SHA256 || current.ActivationID != r.Binding.Requested.ActivationID || current.SourceManifestSHA256 != r.Binding.ManifestSHA256 {
		return false
	}
	if len(r.Effects) < 2 {
		return false
	}
	commit, ready := r.Effects[len(r.Effects)-2], r.Effects[len(r.Effects)-1]
	return commit.Name == "commit" && ready.Name == "ready" && commit.Purpose == "requested" && ready.Purpose == "requested" && commit.State == EffectConfirmed && ready.State == EffectConfirmed && sameTarget(commit.Target, r.Binding.Requested) && commit.Target.LaunchID == current.LaunchID && commit.Target.Complete() && commit.Observed != nil && ready.Observed != nil && *commit.Observed == ready.Before && *ready.Observed == current.Selection && ready.Observed.Current == commit.Target
}

// TransferSelectedResource atomically changes allocated release debt into a
// retained selected resource and persists the exact owner receipt. It never
// changes the immutable operation record or accepts a stale/duplicate receipt.
func (t *Transaction) TransferSelectedResource(receipt SelectionReceipt) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed || t.poisoned || receipt.owner != t || receipt.data.Purpose != "requested" || receipt.data.OperationID != t.record.Request.ID || receipt.data.NamespaceSHA256 != t.namespace || receipt.data.RecordRevision != t.revision || receipt.data.RecordSHA256 != r1RecordDigest(t.record) || !validResourceIdentity(receipt.data.Resource) || receipt.data.Resource.Retention != ResourceReleaseRequired {
		return ErrInvalid
	}
	if !validSelectionReceipt(receipt.data, t.record) {
		return ErrInvalid
	}
	raw, err := json.Marshal(receipt.data)
	if err != nil || len(raw) > maxSelectionReceiptBytes {
		return ErrInvalid
	}
	return t.resourceMutation(func(q resourceQuery) error {
		result, err := q.Exec(`UPDATE resource_obligations SET status=?,cause=?,retention=?,revision=revision+1,selection_receipt=? WHERE operation_id=? AND namespace_sha256=? AND resource_id=? AND status=? AND revision=? AND retention=? AND attempt_id=? AND generation=? AND activation_id=? AND launch_id=? AND image_sha256=? AND source_snapshot_sha256=? AND phase=? AND kind=? AND observer_scope=? AND selection_receipt IS NULL`, ResourceRetained, "selected_receipt", ResourceRetainSelected, raw, receipt.data.OperationID, receipt.data.NamespaceSHA256, receipt.data.ResourceID, ResourceAllocated, receipt.data.ResourceRevision, ResourceReleaseRequired, receipt.data.Resource.AttemptID, receipt.data.Resource.Generation, receipt.data.ActivationID, receipt.data.LaunchID, receipt.data.ImageSHA256, receipt.data.SourceSnapshotSHA256, receipt.data.Resource.Phase, receipt.data.Resource.Kind, receipt.data.Resource.ObserverScope)
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

const maxSelectionReceiptBytes = 4096

func validSelectionReceipt(d selectionReceiptData, r Record) bool {
	i := d.Resource
	return d.Purpose == "requested" && d.OperationID == r.Request.ID && d.NamespaceSHA256 == r.NamespaceSHA256 && d.RecordRevision > 0 && d.ResourceRevision > 0 && shaPattern.MatchString(d.RecordSHA256) && d.PeerPID > 0 && validResourceIdentity(i) && i.Kind == ResourceKindActivation && i.Retention == ResourceReleaseRequired && d.ResourceID == i.ID && d.ActivationID == i.ActivationID && d.LaunchID == i.LaunchID && d.ImageSHA256 == i.ImageSHA256 && d.SourceSnapshotSHA256 == i.SourceSnapshotSHA256 && shaPattern.MatchString(d.SourceSnapshotSHA256) && d.SelectionSHA256 == d.Selection.StateSHA256 && requestedSelectionAcknowledged(r, R1CurrentEvidence{ActivationID: d.ActivationID, LaunchID: d.LaunchID, LoadedImageSHA256: d.ImageSHA256, SourceManifestSHA256: d.ManifestSHA256, Selection: d.Selection})
}

// A terminal outcome is the only allowed difference from the captured record.
// Binding/effects are compared through the historical pending record digest.
func retainedReceiptValid(raw []byte, row ResourceRecord, record Record, revision int64) bool {
	if len(raw) == 0 || len(raw) > maxSelectionReceiptBytes || row.Status != ResourceRetained || row.Retention != ResourceRetainSelected || row.Cause != "selected_receipt" {
		return false
	}
	var d selectionReceiptData
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&d) != nil || decoder.Decode(new(any)) != io.EOF || !validSelectionReceipt(d, record) {
		return false
	}
	original := record
	original.Outcome = Pending
	i := row.ResourceObligation
	i.Retention = ResourceReleaseRequired
	return d.Resource == i && d.OperationID == row.OperationID && d.NamespaceSHA256 == row.NamespaceSHA && d.ResourceRevision+1 == row.Revision && d.RecordRevision < revision && d.RecordSHA256 == r1RecordDigest(original)
}
