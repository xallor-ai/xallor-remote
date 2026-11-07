package access

import (
	"context"

	"github.com/xallor-ai/xallor-remote/internal/principal"
	"github.com/xallor-ai/xallor-remote/internal/protocol"
)

// Manage requires an authenticated device connection's exact identity snapshot.
// The transport alone proves possession; this service never trusts request IDs
// as device proof and never returns a mutation before its commit is confirmed.
func (s *Service) Manage(ctx context.Context, id string, expected Device, actor principal.Identity, req Request) (Mutation, error) {
	if err := req.Validate(); err != nil {
		return Mutation{}, err
	}
	var result Mutation
	err := s.store.WriteAccess(ctx, func(tx Transaction) error {
		if err := s.verifyActor(ctx, tx, actor); err != nil {
			return err
		}
		device, err := tx.DeviceByID(ctx, id)
		if err != nil {
			return err
		}
		if device.UID != expected.UID || device.SecretHash != expected.SecretHash || device.OwnershipID != expected.OwnershipID {
			return ErrDenied
		}
		if device.OwnershipID != "" && !device.OwnedBy(actor) {
			return ErrDenied
		}
		if req.Action != "claim" && req.Action != "status" && device.OwnershipID == "" {
			return ErrDenied
		}
		switch req.Action {
		case "claim":
			if device.OwnershipID == "" {
				err = setOwner(ctx, tx, device.UID, actor.TenantID, actor.PrincipalID)
			}
		case "transfer":
			var member Member
			member, err = activeMember(ctx, tx, req.MembershipID)
			if err == nil && (member.TenantID != device.OwnerTenant || member.PrincipalID != device.OwnerPrincipal) {
				err = setOwner(ctx, tx, device.UID, member.TenantID, member.PrincipalID)
			}
		case "share":
			err = replaceShare(ctx, tx, device, req, s.now().Unix())
		case "unshare":
			err = tx.DeleteShare(ctx, device.OwnershipID, req.ShareID)
		}
		if err != nil {
			return err
		}
		device, err = tx.DeviceByID(ctx, id)
		if err != nil {
			return err
		}
		view, err := state(ctx, tx, device)
		result = Mutation{Device: device, State: view}
		return err
	})
	if err != nil {
		return Mutation{}, err
	}
	return result, nil
}

func activeMember(ctx context.Context, reader Reader, id string) (Member, error) {
	member, err := reader.Member(ctx, id)
	if err != nil {
		return Member{}, err
	}
	if !member.Active {
		return Member{}, ErrDenied
	}
	return member, nil
}

func setOwner(ctx context.Context, tx Transaction, uid, tenant, person string) error {
	id, err := protocol.RandomHex(16)
	if err != nil {
		return err
	}
	return tx.ReplaceOwnership(ctx, Ownership{ID: id, DeviceUID: uid, TenantID: tenant, PrincipalID: person})
}

func replaceShare(ctx context.Context, tx Transaction, device Device, req Request, now int64) error {
	if _, err := activeMember(ctx, tx, req.MembershipID); err != nil {
		return err
	}
	permissions, err := ParsePermissions(req.Operations)
	if err != nil {
		return err
	}
	id, err := protocol.RandomHex(16)
	if err != nil {
		return err
	}
	// A replacement gets a new authorization ID, even for identical permissions.
	if err := tx.DeleteMemberShare(ctx, device.OwnershipID, req.MembershipID); err != nil {
		return err
	}
	if err := tx.DeleteExpiredShares(ctx, device.OwnershipID, now); err != nil {
		return err
	}
	count, err := tx.CountShares(ctx, device.OwnershipID)
	if err != nil {
		return err
	}
	if count >= protocol.MaxDeviceShares {
		return ErrLimit
	}
	return tx.InsertShare(ctx, ShareRecord{ID: id, OwnershipID: device.OwnershipID, MembershipID: req.MembershipID, Permissions: permissions, ExpiresAt: now + req.TTLSeconds})
}
