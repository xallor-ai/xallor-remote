package storage

import (
	"context"
	"database/sql"
	"errors"

	"github.com/xallor-ai/xallor-remote/internal/oidc"
)

func migrateOIDC(tx *sql.Tx) error {
	var version int
	if err := tx.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil {
		return err
	}
	if version >= 7 {
		return nil
	}
	_, err := tx.Exec(`
CREATE TABLE IF NOT EXISTS oidc_issuers (
 issuer TEXT PRIMARY KEY NOT NULL,
 audience TEXT NOT NULL,
 jwks_url TEXT NOT NULL,
 version INTEGER NOT NULL,
 active INTEGER NOT NULL DEFAULT 1,
 expires_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS oidc_subject_mappings (
 issuer TEXT NOT NULL REFERENCES oidc_issuers(issuer),
 subject TEXT NOT NULL,
 tenant_id TEXT NOT NULL REFERENCES tenants(id),
 principal_id TEXT NOT NULL REFERENCES principals(id),
 membership_id TEXT NOT NULL REFERENCES memberships(id),
 membership_version INTEGER NOT NULL,
 version INTEGER NOT NULL,
 active INTEGER NOT NULL DEFAULT 1,
 expires_at INTEGER NOT NULL,
 PRIMARY KEY(issuer,subject)
);
PRAGMA user_version=7;`)
	return err
}

var _ oidc.MappingStore = (*SQLStore)(nil)

func (s *SQLStore) PutIssuer(ctx context.Context, cfg oidc.IssuerConfig) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var previous int64
	err = tx.QueryRowContext(ctx, `SELECT version FROM oidc_issuers WHERE issuer=?`, cfg.Issuer).Scan(&previous)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		_, err = tx.ExecContext(ctx, `INSERT INTO oidc_issuers(issuer,audience,jwks_url,version,active,expires_at) VALUES (?,?,?,?,?,?)`, cfg.Issuer, cfg.Audience, cfg.JWKSURL, cfg.Version, cfg.Active, cfg.ExpiresAt)
	case err != nil:
		return err
	case cfg.Version <= previous:
		return oidc.ErrMappingConflict
	default:
		_, err = tx.ExecContext(ctx, `UPDATE oidc_issuers SET audience=?,jwks_url=?,version=?,active=?,expires_at=? WHERE issuer=?`, cfg.Audience, cfg.JWKSURL, cfg.Version, cfg.Active, cfg.ExpiresAt, cfg.Issuer)
	}
	if err != nil {
		return err
	}
	return tx.Commit()
}

func (s *SQLStore) RevokeIssuer(ctx context.Context, issuer string, version int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var previous int64
	if err := tx.QueryRowContext(ctx, `SELECT version FROM oidc_issuers WHERE issuer=?`, issuer).Scan(&previous); errors.Is(err, sql.ErrNoRows) {
		return oidc.ErrMappingNotFound
	} else if err != nil {
		return err
	} else if version <= previous {
		return oidc.ErrMappingConflict
	}
	if _, err := tx.ExecContext(ctx, `UPDATE oidc_issuers SET active=0,version=? WHERE issuer=?`, version, issuer); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *SQLStore) LoadIssuer(ctx context.Context, issuer string) (oidc.IssuerConfig, error) {
	var cfg oidc.IssuerConfig
	err := s.db.QueryRowContext(ctx, `SELECT issuer,audience,jwks_url,version,active,expires_at FROM oidc_issuers WHERE issuer=?`, issuer).Scan(&cfg.Issuer, &cfg.Audience, &cfg.JWKSURL, &cfg.Version, &cfg.Active, &cfg.ExpiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		return oidc.IssuerConfig{}, oidc.ErrMappingNotFound
	}
	return cfg, err
}

func (s *SQLStore) PutMapping(ctx context.Context, mapping oidc.Mapping) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var tenant, principal string
	var active bool
	var membershipVersion int64
	err = tx.QueryRowContext(ctx, `SELECT tenant_id,principal_id,active,version FROM memberships WHERE id=?`, mapping.MembershipID).Scan(&tenant, &principal, &active, &membershipVersion)
	if errors.Is(err, sql.ErrNoRows) || !active || tenant != mapping.TenantID || principal != mapping.PrincipalID || membershipVersion != mapping.MembershipVersion {
		return oidc.ErrMappingNotFound
	}
	var previous int64
	err = tx.QueryRowContext(ctx, `SELECT version FROM oidc_subject_mappings WHERE issuer=? AND subject=?`, mapping.Issuer, mapping.Value).Scan(&previous)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		_, err = tx.ExecContext(ctx, `INSERT INTO oidc_subject_mappings(issuer,subject,tenant_id,principal_id,membership_id,membership_version,version,active,expires_at) VALUES (?,?,?,?,?,?,?,?,?)`, mapping.Issuer, mapping.Value, mapping.TenantID, mapping.PrincipalID, mapping.MembershipID, mapping.MembershipVersion, mapping.Version, mapping.Active, mapping.ExpiresAt)
	case err != nil:
		return err
	case mapping.Version <= previous:
		return oidc.ErrMappingConflict
	default:
		_, err = tx.ExecContext(ctx, `UPDATE oidc_subject_mappings SET tenant_id=?,principal_id=?,membership_id=?,membership_version=?,version=?,active=?,expires_at=? WHERE issuer=? AND subject=?`, mapping.TenantID, mapping.PrincipalID, mapping.MembershipID, mapping.MembershipVersion, mapping.Version, mapping.Active, mapping.ExpiresAt, mapping.Issuer, mapping.Value)
	}
	if err != nil {
		return err
	}
	return tx.Commit()
}

func (s *SQLStore) RevokeMapping(ctx context.Context, subject oidc.Subject, version int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var previous int64
	if err := tx.QueryRowContext(ctx, `SELECT version FROM oidc_subject_mappings WHERE issuer=? AND subject=?`, subject.Issuer, subject.Value).Scan(&previous); errors.Is(err, sql.ErrNoRows) {
		return oidc.ErrMappingNotFound
	} else if err != nil {
		return err
	} else if version <= previous {
		return oidc.ErrMappingConflict
	}
	if _, err := tx.ExecContext(ctx, `UPDATE oidc_subject_mappings SET active=0,version=? WHERE issuer=? AND subject=?`, version, subject.Issuer, subject.Value); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *SQLStore) LookupMapping(ctx context.Context, subject oidc.Subject) (oidc.Mapping, error) {
	var mapping oidc.Mapping
	var active bool
	err := s.db.QueryRowContext(ctx, `
SELECT x.issuer,x.subject,x.tenant_id,x.principal_id,x.membership_id,x.membership_version,x.version,x.active,x.expires_at
FROM oidc_subject_mappings x
JOIN memberships m ON m.id=x.membership_id
WHERE x.issuer=? AND x.subject=? AND x.tenant_id=m.tenant_id AND x.principal_id=m.principal_id AND x.membership_version=m.version`, subject.Issuer, subject.Value).
		Scan(&mapping.Issuer, &mapping.Value, &mapping.TenantID, &mapping.PrincipalID, &mapping.MembershipID, &mapping.MembershipVersion, &mapping.Version, &active, &mapping.ExpiresAt)
	mapping.Active = active
	if errors.Is(err, sql.ErrNoRows) {
		return oidc.Mapping{}, oidc.ErrMappingNotFound
	}
	return mapping, err
}
