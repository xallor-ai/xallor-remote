package relay

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/xallor-ai/xallor-remote/internal/access"
)

func scanDevice(row *sql.Row) (access.Device, error) {
	var record access.Device
	err := row.Scan(&record.UID, &record.SecretHash, &record.Inbound, &record.GrantID, &record.GrantHash)
	if errors.Is(err, sql.ErrNoRows) {
		return access.Device{}, ErrDeviceNotFound
	}
	return record, err
}

const selectDevice = `SELECT d.uid, d.secret_hash, d.inbound, COALESCE(g.id,''), COALESCE(g.token_hash,'')
FROM devices d LEFT JOIN device_grants g ON g.device_uid=d.uid WHERE d.id=?`

func (s *SQLStore) LoadDevice(ctx context.Context, id string) (access.Device, error) {
	return scanDevice(s.db.QueryRowContext(ctx, selectDevice, id))
}

func (s *SQLStore) RegisterDevice(ctx context.Context, id, secretHash string, inbound bool) (access.Device, error) {
	candidate, err := access.NewDevice(secretHash, inbound)
	if err != nil {
		return access.Device{}, err
	}
	return s.identityTransaction(ctx, func(tx *sql.Tx) (access.Device, error) {
		// A competing registration must never overwrite the winning identity.
		_, err := tx.ExecContext(ctx, `INSERT INTO devices (uid, id, secret_hash, inbound, last_seen) VALUES (?,?,?,?,?)
ON CONFLICT(id) DO NOTHING`, candidate.UID, id, secretHash, inbound, time.Now().Unix())
		if err != nil {
			return access.Device{}, err
		}
		record, err := scanDevice(tx.QueryRowContext(ctx, selectDevice, id))
		if err != nil {
			return access.Device{}, err
		}
		if record.SecretHash != secretHash {
			return access.Device{}, ErrDeviceIdentity
		}
		return record, nil
	})
}

func (s *SQLStore) UpdateDevice(ctx context.Context, id string, requested access.Device) (access.Device, error) {
	return s.identityTransaction(ctx, func(tx *sql.Tx) (access.Device, error) {
		current, err := scanDevice(tx.QueryRowContext(ctx, selectDevice, id))
		if errors.Is(err, ErrDeviceNotFound) {
			return access.Device{}, ErrDeviceIdentity
		}
		if err != nil {
			return access.Device{}, err
		}
		if current.UID != requested.UID || current.SecretHash != requested.SecretHash {
			return access.Device{}, ErrDeviceIdentity
		}
		record, err := current.WithAccess(requested.GrantHash, requested.Inbound)
		if err != nil {
			return access.Device{}, err
		}
		result, err := tx.ExecContext(ctx, `UPDATE devices SET inbound=?, last_seen=?
WHERE uid=? AND secret_hash=?`, record.Inbound, time.Now().Unix(), record.UID, record.SecretHash)
		if err := requireDeviceMutation(result, err); err != nil {
			return access.Device{}, err
		}
		if record.GrantID == "" {
			_, err = tx.ExecContext(ctx, `DELETE FROM device_grants WHERE device_uid=?`, record.UID)
		} else {
			_, err = tx.ExecContext(ctx, `INSERT INTO device_grants (id, device_uid, token_hash) VALUES (?,?,?)
ON CONFLICT(device_uid) DO UPDATE SET id=excluded.id, token_hash=excluded.token_hash`, record.GrantID, record.UID, record.GrantHash)
		}
		return record, err
	})
}

func (s *SQLStore) DeleteDevice(ctx context.Context, id, uid, secretHash string) error {
	_, err := s.identityTransaction(ctx, func(tx *sql.Tx) (access.Device, error) {
		result, err := tx.ExecContext(ctx, `DELETE FROM devices WHERE id=? AND uid=? AND secret_hash=?`, id, uid, secretHash)
		return access.Device{}, requireDeviceMutation(result, err)
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
