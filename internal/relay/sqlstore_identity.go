package relay

import (
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

func (s *SQLStore) LoadDevice(id string) (DeviceRecord, error) {
	return scanDevice(s.db.QueryRow(`SELECT secret_hash, grant_hash, inbound FROM devices WHERE id=?`, id))
}

func (s *SQLStore) RegisterDevice(id, secretHash string, inbound bool) (DeviceRecord, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return DeviceRecord{}, err
	}
	defer tx.Rollback()
	// A competing registration must never overwrite the winning identity.
	_, err = tx.Exec(`INSERT INTO devices (id, secret_hash, inbound, last_seen) VALUES (?,?,?,?)
ON CONFLICT(id) DO NOTHING`, id, secretHash, inbound, time.Now().Unix())
	if err != nil {
		return DeviceRecord{}, err
	}
	record, err := scanDevice(tx.QueryRow(`SELECT secret_hash, grant_hash, inbound FROM devices WHERE id=?`, id))
	if err != nil {
		return DeviceRecord{}, err
	}
	if record.SecretHash != secretHash {
		return DeviceRecord{}, ErrDeviceIdentity
	}
	if err := tx.Commit(); err != nil {
		return DeviceRecord{}, err
	}
	return record, nil
}

func (s *SQLStore) UpdateDevice(id string, record DeviceRecord) error {
	result, err := s.db.Exec(`UPDATE devices SET grant_hash=?, inbound=?, last_seen=?
WHERE id=? AND secret_hash=?`, record.GrantHash, record.Inbound, time.Now().Unix(), id, record.SecretHash)
	return requireDeviceMutation(result, err)
}

func (s *SQLStore) DeleteDevice(id, secretHash string) error {
	result, err := s.db.Exec(`DELETE FROM devices WHERE id=? AND secret_hash=?`, id, secretHash)
	return requireDeviceMutation(result, err)
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
