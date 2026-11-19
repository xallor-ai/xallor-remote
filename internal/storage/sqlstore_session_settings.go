package storage

import (
	"context"
	"database/sql"
	"time"

	"github.com/xallor-ai/xallor-remote/internal/access"
	"github.com/xallor-ai/xallor-remote/internal/accesscode"
)

// The caller's transaction already owns the device/settings mutation. Only an
// explicitly registered consent target participates; an absent row is not an
// implicit feature enablement. Return the same committed epoch snapshot.
func updateSessionSettings(ctx context.Context, tx *sql.Tx, before, after access.Device, proposed accesscode.TargetBinding) (access.Device, error) {
	if before.SessionEpoch == "" {
		return after, nil
	}
	current := accesscode.TargetBinding{DeviceUID: before.UID, Epoch: before.SessionEpoch}
	authority := accessCodeTransaction{tx: tx}
	changed := before.Inbound != after.Inbound || before.Binding() != after.Binding()
	if !after.Inbound {
		if err := authority.RetireSessionTarget(ctx, current, time.Now().UTC()); err != nil {
			return access.Device{}, err
		}
		after.SessionEpochActive = false
	} else if changed || !before.SessionEpochActive {
		if err := authority.ActivateSessionTarget(ctx, proposed, time.Now().UTC()); err != nil {
			return access.Device{}, err
		}
		after.SessionEpoch, after.SessionEpochActive = proposed.Epoch, true
	}
	return after, nil
}
