package storage

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/xallor-ai/xallor-remote/internal/principal"
	"github.com/xallor-ai/xallor-remote/internal/protocol"
)

const principalQuery = `SELECT a.id,m.principal_id,m.tenant_id,m.id,m.version,c.id,c.expires_at
FROM principal_credentials c JOIN memberships m ON m.id=c.membership_id
CROSS JOIN authority a WHERE a.singleton=1 AND c.revoked=0 AND m.active=1 AND c.expires_at>? AND `

func scanPrincipal(row *sql.Row) (principal.Identity, error) {
	var p principal.Identity
	err := row.Scan(&p.Authority, &p.PrincipalID, &p.TenantID, &p.MembershipID, &p.MembershipVersion, &p.CredentialID, &p.ExpiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		return p, principal.ErrUnauthorized
	}
	return p, err
}

func (s *SQLStore) Authenticate(ctx context.Context, token string) (principal.Identity, error) {
	if !principal.ValidToken(token) {
		return principal.Identity{}, principal.ErrUnauthorized
	}
	return scanPrincipal(s.db.QueryRowContext(ctx, principalQuery+`c.token_hash=?`, time.Now().Unix(), protocol.SHA256Hex(token)))
}

func (s *SQLStore) Verify(ctx context.Context, expected principal.Identity) error {
	current, err := scanPrincipal(s.db.QueryRowContext(ctx, principalQuery+`c.id=?`, time.Now().Unix(), expected.CredentialID))
	if err != nil {
		return err
	}
	if current != expected {
		return principal.ErrUnauthorized
	}
	return nil
}
