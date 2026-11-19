package runtimeowner

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"

	"github.com/xallor-ai/xallor-remote/internal/release"
)

// Worker groups describe one admitted lifetime; individual sockets remain
// bounded in memory. These receipts confer no historical process authority.
func (t *Transaction) workerResource(kind, role string) (ResourceObligation, error) {
	t.mu.Lock()
	operation := t.record.Request.ID
	t.mu.Unlock()
	digest := sha256.Sum256([]byte(operation + ":worker:" + role))
	identity := ResourceObligation{ID: hex.EncodeToString(digest[:16]), AttemptID: operation,
		Generation: 1, Phase: ResourcePhaseWorker, Kind: kind, ObserverScope: ObserverScopeOwnerProcess, Retention: ResourceReleaseRequired}
	return identity, t.RecordResourceIntent(identity)
}

func (t *Transaction) settleWorkerResource(identity ResourceObligation, closeErr error) error {
	if closeErr != nil {
		return errors.Join(release.ErrCleanupUncertain, closeErr, t.MarkResourceUnknown(identity.ID, identity, CauseCleanupUnknown))
	}
	return t.CloseResource(identity.ID, identity, ResourceClosure{Cause: CauseClosed})
}
