package runtimeowner

import "github.com/xallor-ai/xallor-remote/internal/release"

// releaseResources adapts release's value-only receipt to the admitted
// transaction. Operation and namespace binding remain transaction-owned.
type releaseResources struct{ transaction *Transaction }

var _ release.ResourceRecorder = releaseResources{}

func releaseResourceIdentity(r release.ResourceObligation) ResourceObligation {
	return ResourceObligation(r)
}

func (r releaseResources) RecordIntent(identity release.ResourceObligation) error {
	return r.transaction.RecordResourceIntent(releaseResourceIdentity(identity))
}

func (r releaseResources) Close(identity release.ResourceObligation) error {
	i := releaseResourceIdentity(identity)
	return r.transaction.CloseResource(i.ID, i, ResourceClosure{Cause: CauseClosed})
}

func (r releaseResources) Unknown(identity release.ResourceObligation, cause string) error {
	i := releaseResourceIdentity(identity)
	return r.transaction.MarkResourceUnknown(i.ID, i, cause)
}

func (r releaseResources) RetainSelected(identity release.ResourceObligation, observation release.ReadyObservation) error {
	evidence := SelectionOwnerEvidence{SourceSnapshotSHA256: observation.SourceSnapshotSHA256, Current: R1CurrentEvidence{OwnerAuthenticated: observation.Authenticated, ReadyStable: true, PeerPID: observation.PeerPID, ActivationID: observation.Selection.Current.ActivationID, LaunchID: observation.Selection.Current.LaunchID, LoadedImageSHA256: observation.ImageSHA256, SourceManifestSHA256: observation.ManifestSHA256, Selection: observation.Selection}}
	receipt, err := r.transaction.SelectionReceipt(ResourceObligation(identity), 1, evidence)
	if err != nil {
		return err
	}
	return r.transaction.TransferSelectedResource(receipt)
}
