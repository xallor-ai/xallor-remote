package release

import "errors"

func activationLaunch(a Activation) (string, error) {
	if a.ownership != nil {
		return a.ownership.obligation.LaunchID, nil
	}
	return activationID()
}

func (s Store) prepareActivationResource(id string, m Manifest, raw []byte) (ResourceObligation, error) {
	resource, attempt, err := resourceIdentity()
	if err != nil {
		return ResourceObligation{}, err
	}
	launch, err := activationID()
	if err != nil {
		return ResourceObligation{}, err
	}
	obligation := ResourceObligation{ID: resource, AttemptID: attempt, Generation: 1, Phase: "effects", Kind: "activation", ActivationID: id, LaunchID: launch, ImageSHA256: m.Artifact.SHA256, SourceSnapshotSHA256: snapshotDigest(raw, m.Artifact.SHA256), ObserverScope: resourceScopeOwner, Retention: resourceRelease}
	if s.resources != nil {
		if err := s.resources.RecordIntent(obligation); err != nil {
			return ResourceObligation{}, errors.Join(ErrCleanupUncertain, err)
		}
	}
	return obligation, nil
}

func (s Store) settleActivation(a Activation, cleanup error) error {
	if a.ownership == nil || a.ownership.obligation.ID == "" {
		return cleanup
	}
	return resourceCleanup(s.resources, a.ownership.obligation, cleanup)
}
