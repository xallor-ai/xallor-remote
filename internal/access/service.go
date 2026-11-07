package access

import (
	"context"
	"time"

	"github.com/xallor-ai/xallor-remote/internal/principal"
)

// Service owns device access decisions. It consumes trusted connection proof
// from the transport and revalidates that proof against transactional state.
type Service struct {
	store Store
	now   func() time.Time
}

func NewService(store Store) *Service { return &Service{store: store, now: time.Now} }

func (s *Service) verifyActor(ctx context.Context, reader Reader, actor principal.Identity) error {
	current, err := reader.CredentialByID(ctx, actor.CredentialID)
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if current.Verify(actor, s.now().Unix()) != nil {
		return ErrDenied
	}
	return nil
}

func (s *Service) resolve(ctx context.Context, reader Reader, uid string, actor principal.Identity) (Authorization, error) {
	device, err := reader.DeviceByUID(ctx, uid)
	if err != nil {
		return Authorization{}, err
	}
	if device.OwnershipID == "" {
		return Authorization{}, ErrDenied
	}
	if err := s.verifyActor(ctx, reader, actor); err != nil {
		return Authorization{}, err
	}
	a := Authorization{Binding: device.Binding()}
	if device.OwnedBy(actor) {
		a.Permissions = AllPermissions()
		return a, nil
	}
	share, err := reader.Share(ctx, device.OwnershipID, actor.MembershipID)
	if err != nil {
		return Authorization{}, err
	}
	if share.ExpiresAt <= s.now().Unix() {
		return Authorization{}, ErrDenied
	}
	a.ShareID, a.Permissions, a.ExpiresAt = share.ID, share.Permissions, share.ExpiresAt
	return a, nil
}

func (s *Service) Resolve(ctx context.Context, expected Device, actor principal.Identity) (Authorization, error) {
	var result Authorization
	err := s.store.ReadAccess(ctx, func(reader Reader) error {
		var err error
		result, err = s.resolve(ctx, reader, expected.UID, actor)
		if err != nil {
			return err
		}
		if result.Binding != expected.Binding() {
			return ErrDenied
		}
		return nil
	})
	if err != nil {
		return Authorization{}, err
	}
	return result, nil
}

func (s *Service) Verify(ctx context.Context, actor principal.Identity, expected Authorization, operation string) error {
	return s.store.ReadAccess(ctx, func(reader Reader) error {
		current, err := s.resolve(ctx, reader, expected.Binding.DeviceUID, actor)
		if err != nil {
			return err
		}
		if current != expected || !current.Permissions.Allows(operation) {
			return ErrDenied
		}
		return nil
	})
}

func state(ctx context.Context, reader Reader, device Device) (State, error) {
	result := State{DeviceUID: device.UID, OwnershipID: device.OwnershipID, OwnerTenant: device.OwnerTenant, OwnerPrincipal: device.OwnerPrincipal, Shares: []Share{}}
	shares, err := reader.Shares(ctx, device.OwnershipID)
	if err != nil {
		return State{}, err
	}
	for _, share := range shares {
		result.Shares = append(result.Shares, Share{ID: share.ID, MembershipID: share.MembershipID, Operations: share.Permissions.Names(), ExpiresAt: share.ExpiresAt})
	}
	return result, nil
}
