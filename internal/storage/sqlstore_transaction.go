package storage

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
func storeTransaction[T any](ctx context.Context, db *sql.DB, operation func(*sql.Tx) (T, error)) (T, error) {
	var zero T
	for {
		if err := ctx.Err(); err != nil {
			return zero, err
		}
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return zero, err
		}
		record, err := operation(tx)
		if err == nil {
			if err := tx.Commit(); err != nil {
				return zero, err
			}
			return record, nil
		}
		rollbackErr := tx.Rollback()
		if ctx.Err() != nil {
			return zero, ctx.Err()
		}
		var failure *sqlite.Error
		if rollbackErr != nil || !errors.As(err, &failure) || failure.Code()&0xff != sqlite3.SQLITE_BUSY {
			return zero, err
		}
		timer := time.NewTimer(10 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return zero, ctx.Err()
		case <-timer.C:
		}
	}
}
