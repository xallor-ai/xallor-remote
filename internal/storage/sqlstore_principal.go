package storage

import (
	"context"
	"database/sql"
	"errors"

	"github.com/xallor-ai/xallor-remote/internal/principal"
)

var _ principal.Reader = (*SQLStore)(nil)

const principalQuery = `SELECT a.id,m.principal_id,m.tenant_id,m.id,m.version,c.id,c.expires_at,c.revoked,m.active
FROM principal_credentials c JOIN memberships m ON m.id=c.membership_id
CROSS JOIN authority a WHERE a.singleton=1 AND `

func scanPrincipal(row *sql.Row) (principal.Credential, error) {
	var p principal.Credential
	err := row.Scan(&p.Authority, &p.PrincipalID, &p.TenantID, &p.MembershipID, &p.MembershipVersion, &p.CredentialID, &p.ExpiresAt, &p.Revoked, &p.MembershipActive)
	if errors.Is(err, sql.ErrNoRows) {
		return p, principal.ErrUnauthorized
	}
	return p, err
}

func (s *SQLStore) CredentialByTokenHash(ctx context.Context, hash string) (principal.Credential, error) {
	return scanPrincipal(s.db.QueryRowContext(ctx, principalQuery+`c.token_hash=?`, hash))
}

func (s *SQLStore) CredentialByID(ctx context.Context, id string) (principal.Credential, error) {
	return scanPrincipal(s.db.QueryRowContext(ctx, principalQuery+`c.id=?`, id))
}
