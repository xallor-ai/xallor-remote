package relay

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"

	"github.com/xallor-ai/xallor-remote/internal/access"
)

// Retry only busy operations whose transaction was explicitly rolled back.
// Commit errors are returned as-is: an uncertain commit must never be replayed.
func (s *SQLStore) identityTransaction(ctx context.Context, operation func(*sql.Tx) (access.Device, error)) (access.Device, error) {
	for {
		if err := ctx.Err(); err != nil {
			return access.Device{}, err
		}
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return access.Device{}, err
		}
		record, err := operation(tx)
		if err == nil {
			if err := tx.Commit(); err != nil {
				return access.Device{}, err
			}
			return record, nil
		}
		rollbackErr := tx.Rollback()
		if ctx.Err() != nil {
			return access.Device{}, ctx.Err()
		}
		var failure *sqlite.Error
		if rollbackErr != nil || !errors.As(err, &failure) || failure.Code()&0xff != sqlite3.SQLITE_BUSY {
			return access.Device{}, err
		}
		timer := time.NewTimer(10 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return access.Device{}, ctx.Err()
		case <-timer.C:
		}
	}
}
