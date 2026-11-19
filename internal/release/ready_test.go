package release

import (
	"errors"
	"testing"
)

type partialReadyControl struct{ *fakeControl }

func (c partialReadyControl) PrepareReady(r ReadyRequest) (ReadyAttempt, error) {
	return fixtureReady{r, func() error { return errors.New("must not dispatch partial preparation") }}, errors.New("partial preparation")
}

// Partial preparation has an explicit exact cleanup receipt; release must use
// it even though observation never begins and the selected process stays alive.
func TestReady_shouldSettleExactPartialPreparationReceipt(t *testing.T) {
	dir, m := bundle(t, "partial-ready")
	s := mechanicsStore(t)
	ledger := &resourceTrace{}
	c := partialReadyControl{control(m)}
	if err := s.WithResources(ledger).Install(dir, c); !errors.Is(err, ErrCleanupUncertain) {
		t.Fatal(err)
	}
	var observer ResourceObligation
	for _, i := range ledger.intent {
		if i.Kind == "observer" {
			observer = i
		}
	}
	for _, i := range ledger.closed {
		if i == observer && i.ID != "" {
			return
		}
	}
	t.Fatal("partial preparation cleanup receipt was ignored", ledger)
}
