package relay

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)

// Retry only busy operations whose transaction was explicitly rolled back.
// Commit errors are returned as-is: an uncertain commit must never be replayed.
func (s *SQLStore) identityTransaction(ctx context.Context, operation func(*sql.Tx) (DeviceRecord, error)) (DeviceRecord, error) {
	for {
		if err := ctx.Err(); err != nil {
			return DeviceRecord{}, err
		}
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return DeviceRecord{}, err
		}
		record, err := operation(tx)
		if err == nil {
			if err := tx.Commit(); err != nil {
				return DeviceRecord{}, err
			}
			return record, nil
		}
		rollbackErr := tx.Rollback()
		if ctx.Err() != nil {
			return DeviceRecord{}, ctx.Err()
		}
		var failure *sqlite.Error
		if rollbackErr != nil || !errors.As(err, &failure) || failure.Code()&0xff != sqlite3.SQLITE_BUSY {
			return DeviceRecord{}, err
		}
		timer := time.NewTimer(10 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return DeviceRecord{}, ctx.Err()
		case <-timer.C:
		}
	}
}
