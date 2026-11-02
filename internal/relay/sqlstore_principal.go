package relay

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
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

// EnrollPrincipal is a local Relay-operator operation. It is never exposed on the
// public websocket. An existing tenant is selected only by the trusted operator.
func (s *SQLStore) EnrollPrincipal(ctx context.Context, name, kind, tenant string, ttl time.Duration) (principal.Enrollment, error) {
	if err := principal.ValidateEnrollment(name, kind, ttl); err != nil {
		return principal.Enrollment{}, err
	}
	var ids [4]string
	for i := range ids {
		id, err := protocol.RandomHex(16)
		if err != nil {
			return principal.Enrollment{}, err
		}
		ids[i] = id
	}
	token, err := principal.NewToken()
	if err != nil {
		return principal.Enrollment{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return principal.Enrollment{}, err
	}
	defer tx.Rollback()
	role := "member"
	if tenant == "" {
		tenant, role = ids[1], "owner"
		if _, err := tx.ExecContext(ctx, `INSERT INTO tenants(id,kind,name) VALUES (?,'personal',?)`, tenant, name); err != nil {
			return principal.Enrollment{}, err
		}
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO principals(id,kind,name) VALUES (?,?,?)`, ids[0], kind, name); err != nil {
		return principal.Enrollment{}, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO memberships(id,tenant_id,principal_id,role) VALUES (?,?,?,?)`, ids[2], tenant, ids[0], role); err != nil {
		return principal.Enrollment{}, err
	}
	expires := time.Now().Add(ttl).Unix()
	if _, err := tx.ExecContext(ctx, `INSERT INTO principal_credentials(id,token_hash,membership_id,expires_at) VALUES (?,?,?,?)`, ids[3], protocol.SHA256Hex(token), ids[2], expires); err != nil {
		return principal.Enrollment{}, err
	}
	p, err := scanPrincipal(tx.QueryRowContext(ctx, principalQuery+`c.id=?`, time.Now().Unix(), ids[3]))
	if err != nil {
		return principal.Enrollment{}, err
	}
	if err := tx.Commit(); err != nil {
		return principal.Enrollment{}, err
	}
	return principal.Enrollment{Identity: p, Token: token}, nil
}

func (s *SQLStore) RevokeCredential(ctx context.Context, id string) error {
	result, err := s.db.ExecContext(ctx, `UPDATE principal_credentials SET revoked=1 WHERE id=?`, id)
	return requirePrincipalMutation(result, err)
}

func (s *SQLStore) CreateOrganization(ctx context.Context, name string) (string, error) {
	if strings.TrimSpace(name) == "" || len(name) > 200 {
		return "", fmt.Errorf("需要有效的空间名称")
	}
	id, err := protocol.RandomHex(16)
	if err != nil {
		return "", err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO tenants(id,kind,name) VALUES (?,'organization',?)`, id, name)
	return id, err
}

// IssueCredential renews access for the same membership without inventing a new
// principal. Earlier credentials remain separately revocable by credential ID.
func (s *SQLStore) IssueCredential(ctx context.Context, membership string, ttl time.Duration) (principal.Enrollment, error) {
	if err := principal.ValidateEnrollment("credential", "user", ttl); err != nil {
		return principal.Enrollment{}, err
	}
	id, err := protocol.RandomHex(16)
	if err != nil {
		return principal.Enrollment{}, err
	}
	token, err := principal.NewToken()
	if err != nil {
		return principal.Enrollment{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return principal.Enrollment{}, err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `INSERT INTO principal_credentials(id,token_hash,membership_id,expires_at) VALUES (?,?,?,?)`, id, protocol.SHA256Hex(token), membership, time.Now().Add(ttl).Unix()); err != nil {
		return principal.Enrollment{}, err
	}
	p, err := scanPrincipal(tx.QueryRowContext(ctx, principalQuery+`c.id=?`, time.Now().Unix(), id))
	if err != nil {
		return principal.Enrollment{}, err
	}
	if err := tx.Commit(); err != nil {
		return principal.Enrollment{}, err
	}
	return principal.Enrollment{Identity: p, Token: token}, nil
}

func (s *SQLStore) SetMembershipActive(ctx context.Context, id string, active bool) error {
	result, err := s.db.ExecContext(ctx, `UPDATE memberships SET active=?,version=version+1 WHERE id=?`, active, id)
	return requirePrincipalMutation(result, err)
}

func requirePrincipalMutation(result sql.Result, err error) error {
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return principal.ErrUnauthorized
	}
	return nil
}
