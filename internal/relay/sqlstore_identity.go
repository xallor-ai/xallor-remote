package relay

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

func scanDevice(row *sql.Row) (DeviceRecord, error) {
	var record DeviceRecord
	err := row.Scan(&record.SecretHash, &record.GrantHash, &record.Inbound)
	if errors.Is(err, sql.ErrNoRows) {
		return DeviceRecord{}, ErrDeviceNotFound
	}
	return record, err
}

func (s *SQLStore) LoadDevice(ctx context.Context, id string) (DeviceRecord, error) {
	return scanDevice(s.db.QueryRowContext(ctx, `SELECT secret_hash, grant_hash, inbound FROM devices WHERE id=?`, id))
}

func (s *SQLStore) RegisterDevice(ctx context.Context, id, secretHash string, inbound bool) (DeviceRecord, error) {
	return s.identityTransaction(ctx, func(tx *sql.Tx) (DeviceRecord, error) {
		// A competing registration must never overwrite the winning identity.
		_, err := tx.ExecContext(ctx, `INSERT INTO devices (id, secret_hash, inbound, last_seen) VALUES (?,?,?,?)
ON CONFLICT(id) DO NOTHING`, id, secretHash, inbound, time.Now().Unix())
		if err != nil {
			return DeviceRecord{}, err
		}
		record, err := scanDevice(tx.QueryRowContext(ctx, `SELECT secret_hash, grant_hash, inbound FROM devices WHERE id=?`, id))
		if err != nil {
			return DeviceRecord{}, err
		}
		if record.SecretHash != secretHash {
			return DeviceRecord{}, ErrDeviceIdentity
		}
		return record, nil
	})
}

func (s *SQLStore) UpdateDevice(ctx context.Context, id string, record DeviceRecord) error {
	_, err := s.identityTransaction(ctx, func(tx *sql.Tx) (DeviceRecord, error) {
		result, err := tx.ExecContext(ctx, `UPDATE devices SET grant_hash=?, inbound=?, last_seen=?
WHERE id=? AND secret_hash=?`, record.GrantHash, record.Inbound, time.Now().Unix(), id, record.SecretHash)
		return DeviceRecord{}, requireDeviceMutation(result, err)
	})
	return err
}

func (s *SQLStore) DeleteDevice(ctx context.Context, id, secretHash string) error {
	_, err := s.identityTransaction(ctx, func(tx *sql.Tx) (DeviceRecord, error) {
		result, err := tx.ExecContext(ctx, `DELETE FROM devices WHERE id=? AND secret_hash=?`, id, secretHash)
		return DeviceRecord{}, requireDeviceMutation(result, err)
	})
	return err
}

func requireDeviceMutation(result sql.Result, err error) error {
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("device mutation result: %w", err)
	}
	if n != 1 {
		return ErrDeviceIdentity
	}
	return nil
}
