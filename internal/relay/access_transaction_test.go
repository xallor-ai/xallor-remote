package relay

import (
	"context"
	"errors"
	"testing"

	"github.com/xallor-ai/xallor-remote/internal/access"
)

// Replacement first removes a prior share, then writes the new one. A late
// write failure must restore the old grant and its usable authorization.
func TestAccess_should_retainPriorShare_whenReplacementWriteFails(t *testing.T) {
	s := openIdentityTestStore(t)
	ctx := context.Background()
	owner := enrollment(t, s, "owner", "")
	guest := enrollment(t, s, "guest", "")
	device, err := s.RegisterDevice(ctx, "device", "proof", true)
	if err != nil {
		t.Fatal(err)
	}
	service := access.NewService(s)
	claim, err := service.Manage(ctx, "device", device, owner.Identity, access.Request{Action: "claim"})
	if err != nil {
		t.Fatal(err)
	}
	device = claim.Device
	req := access.Request{Action: "share", MembershipID: guest.MembershipID, Operations: []string{"read"}, TTLSeconds: 60}
	if _, err := service.Manage(ctx, "device", device, owner.Identity, req); err != nil {
		t.Fatal(err)
	}
	before, err := service.Resolve(ctx, device, guest.Identity)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`CREATE TRIGGER reject_share BEFORE INSERT ON device_shares BEGIN SELECT RAISE(ABORT,'injected share failure'); END`); err != nil {
		t.Fatal(err)
	}
	req.Operations = []string{"exec"}
	result, err := service.Manage(ctx, "device", device, owner.Identity, req)
	if err == nil || result.Device.UID != "" {
		t.Fatal("uncommitted grant returned")
	}
	after, err := service.Resolve(ctx, device, guest.Identity)
	if err != nil || after != before || service.Verify(ctx, guest.Identity, before, "read") != nil {
		t.Fatal("replacement destroyed prior share", err)
	}
	if !errors.Is(service.Verify(ctx, guest.Identity, after, "exec"), access.ErrDenied) {
		t.Fatal("failed replacement changed permissions")
	}
}

// A caller's ownership display fields are not authorization facts. Reloading
// the device in the access snapshot prevents forged or stale ownership data.
func TestAccess_should_resolveFromStoredOwnership_whenSnapshotFieldsAreForged(t *testing.T) {
	s := openIdentityTestStore(t)
	ctx := context.Background()
	owner := enrollment(t, s, "owner", "")
	guest := enrollment(t, s, "guest", "")
	device, err := s.RegisterDevice(ctx, "device", "proof", true)
	if err != nil {
		t.Fatal(err)
	}
	device = manageAccess(t, s, device, owner.Identity, access.Request{Action: "claim"}).Device
	device.OwnerTenant, device.OwnerPrincipal = guest.TenantID, guest.PrincipalID
	if _, err := access.NewService(s).Resolve(ctx, device, guest.Identity); !errors.Is(err, access.ErrDenied) {
		t.Fatal("forged device owner trusted", err)
	}
}
