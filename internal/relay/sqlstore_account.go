package relay

import (
	"context"
	"database/sql"
	"errors"

	"github.com/xallor-ai/xallor-remote/internal/account"
	"github.com/xallor-ai/xallor-remote/internal/principal"
)

var _ account.Store = (*SQLStore)(nil)

// TransactAccount exposes atomic persistence to account use cases. A commit
// error is returned without retry, even if the server may have committed it.
func (s *SQLStore) TransactAccount(ctx context.Context, operation func(account.Transaction) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := operation(accountTransaction{tx}); err != nil {
		return err
	}
	return tx.Commit()
}

type accountTransaction struct{ tx *sql.Tx }

func (t accountTransaction) InsertTenant(ctx context.Context, r account.Tenant) error {
	_, err := t.tx.ExecContext(ctx, `INSERT INTO tenants(id,kind,name) VALUES (?,?,?)`, r.ID, r.Kind, r.Name)
	return err
}

func (t accountTransaction) InsertPrincipal(ctx context.Context, r account.Principal) error {
	_, err := t.tx.ExecContext(ctx, `INSERT INTO principals(id,kind,name) VALUES (?,?,?)`, r.ID, r.Kind, r.Name)
	return err
}

func (t accountTransaction) InsertMembership(ctx context.Context, r account.Membership) error {
	_, err := t.tx.ExecContext(ctx, `INSERT INTO memberships(id,tenant_id,principal_id,role,active,version) VALUES (?,?,?,?,?,?)`, r.ID, r.TenantID, r.PrincipalID, r.Role, r.Active, r.Version)
	return err
}

func (t accountTransaction) LoadMembership(ctx context.Context, id string) (account.Membership, error) {
	var r account.Membership
	err := t.tx.QueryRowContext(ctx, `SELECT id,tenant_id,principal_id,role,active,version FROM memberships WHERE id=?`, id).Scan(&r.ID, &r.TenantID, &r.PrincipalID, &r.Role, &r.Active, &r.Version)
	if errors.Is(err, sql.ErrNoRows) {
		return r, principal.ErrUnauthorized
	}
	return r, err
}

func (t accountTransaction) InsertCredential(ctx context.Context, r account.Credential) error {
	_, err := t.tx.ExecContext(ctx, `INSERT INTO principal_credentials(id,token_hash,membership_id,expires_at) VALUES (?,?,?,?)`, r.ID, r.TokenHash, r.MembershipID, r.ExpiresAt)
	return err
}

func (t accountTransaction) RevokeCredential(ctx context.Context, id string) error {
	result, err := t.tx.ExecContext(ctx, `UPDATE principal_credentials SET revoked=1 WHERE id=?`, id)
	return requirePrincipalMutation(result, err)
}

func (t accountTransaction) SetMembershipActive(ctx context.Context, id string, active bool) error {
	result, err := t.tx.ExecContext(ctx, `UPDATE memberships SET active=?,version=version+1 WHERE id=?`, active, id)
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
