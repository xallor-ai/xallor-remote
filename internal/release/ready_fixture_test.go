package release

import (
	"context"
	"errors"
	"os"
	"path/filepath"
)

// Synthetic process identity is confined to mechanics fixtures.
type fixtureReady struct {
	request     ReadyRequest
	acknowledge func() error
}

func (a fixtureReady) Run(context.Context) (ReadyObservation, error) {
	if err := a.acknowledge(); err != nil {
		return ReadyObservation{}, err
	}
	raw, err := os.ReadFile(filepath.Join(filepath.Dir(a.request.Binary), "manifest.json"))
	if err != nil {
		return ReadyObservation{}, err
	}
	return ReadyObservation{Selection: a.request.Selection, PeerPID: 123, ImageSHA256: a.request.Manifest.Artifact.SHA256, ManifestSHA256: rawManifestSHA(raw), SourceSnapshotSHA256: snapshotDigest(raw, a.request.Manifest.Artifact.SHA256), Authenticated: true}, nil
}
func (a fixtureReady) Close() ([]ReadyClosure, error) {
	return []ReadyClosure{{Resource: a.request.Observer, Closed: true}}, nil
}
func (f *fakeControl) PrepareReady(r ReadyRequest) (ReadyAttempt, error) {
	return fixtureReady{r, f.acknowledge}, nil
}
func (c *commitAckControl) PrepareReady(r ReadyRequest) (ReadyAttempt, error) {
	return fixtureReady{r, c.acknowledge}, nil
}
func (r *resourceTrace) RetainSelected(ResourceObligation, ReadyObservation) error { return nil }
func (e *effectTrace) ApplyReady(i EffectIntent, r ReadyRequest, a ReadyAttempt, observe func() (SelectionEvidence, error)) (o ReadyObservation, err error) {
	err = e.Apply(i, func() error {
		var runErr error
		o, runErr = a.Run(context.Background())
		_, closeErr := a.Close()
		return errors.Join(runErr, closeErr)
	}, observe)
	return
}
func (e *bindingTrace) ApplyReady(i EffectIntent, r ReadyRequest, a ReadyAttempt, observe func() (SelectionEvidence, error)) (ReadyObservation, error) {
	e.intents = append(e.intents, i)
	return e.effectTrace.ApplyReady(i, r, a, observe)
}
