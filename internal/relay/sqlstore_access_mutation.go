package relay

import (
	"context"
	"database/sql"
	"time"

	"github.com/xallor-ai/xallor-remote/internal/access"
	"github.com/xallor-ai/xallor-remote/internal/principal"
	"github.com/xallor-ai/xallor-remote/internal/protocol"
)

// ManageAccess is entered only through an authenticated device connection.
// The transaction rechecks both that exact device incarnation and the actor.
func (s *SQLStore) ManageAccess(ctx context.Context, id string, expected access.Device, actor principal.Identity, req access.Request) (access.Mutation, error) {
	if err := req.Validate(); err != nil {
		return access.Mutation{}, err
	}
	return storeTransaction(ctx, s.db, func(tx *sql.Tx) (access.Mutation, error) {
		p, err := scanPrincipal(tx.QueryRowContext(ctx, principalQuery+`c.id=?`, time.Now().Unix(), actor.CredentialID))
		if err != nil {
			return access.Mutation{}, err
		}
		if p != actor {
			return access.Mutation{}, access.ErrDenied
		}
		device, err := scanDevice(tx.QueryRowContext(ctx, selectDevice, id))
		if err != nil {
			return access.Mutation{}, err
		}
		if device.UID != expected.UID || device.SecretHash != expected.SecretHash || device.OwnershipID != expected.OwnershipID {
			return access.Mutation{}, access.ErrDenied
		}
		if device.OwnershipID != "" && !device.OwnedBy(actor) {
			return access.Mutation{}, access.ErrDenied
		}
		if req.Action != "claim" && req.Action != "status" && device.OwnershipID == "" {
			return access.Mutation{}, access.ErrDenied
		}
		switch req.Action {
		case "claim":
			if device.OwnershipID == "" {
				err = replaceOwnership(ctx, tx, device.UID, actor.TenantID, actor.PrincipalID)
			}
		case "transfer":
			var tenant, person string
			tenant, person, err = activeMember(ctx, tx, req.MembershipID)
			if err == nil && (tenant != device.OwnerTenant || person != device.OwnerPrincipal) {
				err = replaceOwnership(ctx, tx, device.UID, tenant, person)
			}
		case "share":
			err = replaceShare(ctx, tx, device, req)
		case "unshare":
			var result sql.Result
			result, err = tx.ExecContext(ctx, `DELETE FROM device_shares WHERE id=? AND ownership_id=?`, req.ShareID, device.OwnershipID)
			if err == nil {
				err = requireDeviceMutation(result, nil)
			}
		}
		if err != nil {
			return access.Mutation{}, err
		}
		device, err = scanDevice(tx.QueryRowContext(ctx, selectDevice, id))
		if err != nil {
			return access.Mutation{}, err
		}
		state, err := accessState(ctx, tx, device)
		return access.Mutation{Device: device, State: state}, err
	})
}

func replaceOwnership(ctx context.Context, tx *sql.Tx, uid, tenant, person string) error {
	id, err := protocol.RandomHex(16)
	if err != nil {
		return err
	}
	// Deleting the previous ownership cascades its shares. A transfer back to the
	// original owner still receives a new ID and cannot revive former connections.
	if _, err := tx.ExecContext(ctx, `DELETE FROM device_ownership WHERE device_uid=?`, uid); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO device_ownership(device_uid,id,tenant_id,principal_id) VALUES (?,?,?,?)`, uid, id, tenant, person)
	return err
}

func replaceShare(ctx context.Context, tx *sql.Tx, device access.Device, req access.Request) error {
	if _, _, err := activeMember(ctx, tx, req.MembershipID); err != nil {
		return err
	}
	permissions, err := access.ParsePermissions(req.Operations)
	if err != nil {
		return err
	}
	id, err := protocol.RandomHex(16)
	if err != nil {
		return err
	}
	// Replacing even an identical share deliberately advances its authorization ID.
	if _, err := tx.ExecContext(ctx, `DELETE FROM device_shares WHERE ownership_id=? AND membership_id=?`, device.OwnershipID, req.MembershipID); err != nil {
		return err
	}
	// Expired grants do not consume the bounded active share inventory.
	if _, err := tx.ExecContext(ctx, `DELETE FROM device_shares WHERE ownership_id=? AND expires_at<=?`, device.OwnershipID, time.Now().Unix()); err != nil {
		return err
	}
	var count int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM device_shares WHERE ownership_id=?`, device.OwnershipID).Scan(&count); err != nil {
		return err
	}
	if count >= protocol.MaxDeviceShares {
		return access.ErrLimit
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO device_shares(id,ownership_id,membership_id,permissions,expires_at) VALUES (?,?,?,?,?)`, id, device.OwnershipID, req.MembershipID, permissions, time.Now().Unix()+req.TTLSeconds)
	return err
}
