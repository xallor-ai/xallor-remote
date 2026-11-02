package relay

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/xallor-ai/xallor-remote/internal/access"
	"github.com/xallor-ai/xallor-remote/internal/account"
	"github.com/xallor-ai/xallor-remote/internal/principal"
)

func enrollment(t *testing.T, s *SQLStore, name, tenant string) principal.Enrollment {
	t.Helper()
	p, err := account.New(s).CreatePrincipal(context.Background(), account.CreatePrincipalRequest{Name: name, Kind: "user", TenantID: tenant, TTL: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func manageAccess(t *testing.T, s *SQLStore, device access.Device, p principal.Identity, req access.Request) access.Mutation {
	t.Helper()
	r, err := s.ManageAccess(context.Background(), "device", device, p, req)
	if err != nil {
		t.Fatal(req.Action, err)
	}
	return r
}

// Knowing a device's bearer or sharing its tenant does not authorize an owned
// device. Explicit grants constrain operations and have non-reusable identities.
func TestAccess_should_isolatePrincipalsAndRevokeOldBindings_whenSharingChanges(t *testing.T) {
	s := openIdentityTestStore(t)
	ctx := context.Background()
	a := enrollment(t, s, "owner", "")
	b := enrollment(t, s, "guest", "")
	c := enrollment(t, s, "same-tenant", a.TenantID)
	device, err := s.RegisterDevice(ctx, "device", "secret", true)
	if err != nil {
		t.Fatal(err)
	}
	device = setTestAccess(t, s, "device", "bearer", true)
	claimed := manageAccess(t, s, device, a.Identity, access.Request{Action: "claim"})
	device = claimed.Device
	if device.OwnerTenant != a.TenantID || device.OwnerPrincipal != a.PrincipalID || device.OwnershipID == "" {
		t.Fatal("untrusted ownership")
	}
	again := manageAccess(t, s, device, a.Identity, access.Request{Action: "claim"})
	if again.Device != device {
		t.Fatal("idempotent claim changed binding")
	}
	for _, denied := range []principal.Identity{b.Identity, c.Identity, {}} {
		if _, err := s.ResolveAccess(ctx, device, denied); err == nil {
			t.Fatal("implicit device access")
		}
	}
	forged := a.Identity
	forged.TenantID = b.TenantID
	if _, err := s.ManageAccess(ctx, "device", device, forged, access.Request{Action: "status"}); err == nil {
		t.Fatal("forged tenant accepted")
	}
	wrong := device
	wrong.UID = "recreated"
	if _, err := s.ManageAccess(ctx, "device", wrong, a.Identity, access.Request{Action: "status"}); err == nil {
		t.Fatal("wrong device incarnation accepted")
	}
	wrong = device
	wrong.SecretHash = "stolen-display-number"
	if _, err := s.ManageAccess(ctx, "device", wrong, a.Identity, access.Request{Action: "status"}); err == nil {
		t.Fatal("wrong device proof accepted")
	}
	shared := manageAccess(t, s, device, a.Identity, access.Request{Action: "share", MembershipID: b.MembershipID, Operations: []string{"info", "read"}, TTLSeconds: 60})
	auth, err := s.ResolveAccess(ctx, device, b.Identity)
	if err != nil || s.VerifyAccess(ctx, b.Identity, auth, "read") != nil || !errors.Is(s.VerifyAccess(ctx, b.Identity, auth, "exec"), access.ErrDenied) {
		t.Fatal("permission scope failed", err)
	}
	manageAccess(t, s, device, a.Identity, access.Request{Action: "unshare", ShareID: shared.State.Shares[0].ID})
	manageAccess(t, s, device, a.Identity, access.Request{Action: "share", MembershipID: b.MembershipID, Operations: []string{"info", "read"}, TTLSeconds: 60})
	if s.VerifyAccess(ctx, b.Identity, auth, "read") == nil {
		t.Fatal("regrant revived old binding")
	}
	fresh, err := s.ResolveAccess(ctx, device, b.Identity)
	if err != nil || fresh.ShareID == auth.ShareID {
		t.Fatal("new grant missing", err)
	}
	if _, err := s.db.Exec(`UPDATE device_shares SET expires_at=? WHERE id=?`, time.Now().Unix()-1, fresh.ShareID); err != nil {
		t.Fatal(err)
	}
	if s.VerifyAccess(ctx, b.Identity, fresh, "read") == nil {
		t.Fatal("expired grant remained valid")
	}
}

// Transfer removes all previous shares transactionally. Transferring back keeps
// the original user's identity but cannot restore their old device authorization.
func TestAccess_should_replaceOwnershipAndShares_whenTransferred(t *testing.T) {
	s := openIdentityTestStore(t)
	ctx := context.Background()
	a := enrollment(t, s, "alice", "")
	b := enrollment(t, s, "bob", "")
	device, err := s.RegisterDevice(ctx, "device", "secret", true)
	if err != nil {
		t.Fatal(err)
	}
	device = manageAccess(t, s, device, a.Identity, access.Request{Action: "claim"}).Device
	old, err := s.ResolveAccess(ctx, device, a.Identity)
	if err != nil {
		t.Fatal(err)
	}
	manageAccess(t, s, device, a.Identity, access.Request{Action: "share", MembershipID: b.MembershipID, Operations: []string{"info"}, TTLSeconds: 60})
	if _, err := s.db.Exec(`CREATE TRIGGER reject_transfer BEFORE INSERT ON device_ownership BEGIN SELECT RAISE(ABORT,'fixture'); END;`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ManageAccess(ctx, "device", device, a.Identity, access.Request{Action: "transfer", MembershipID: b.MembershipID}); err == nil {
		t.Fatal("expected transfer failure")
	}
	retained := manageAccess(t, s, device, a.Identity, access.Request{Action: "status"})
	if retained.Device != device || len(retained.State.Shares) != 1 {
		t.Fatal("partial transfer escaped")
	}
	if _, err := s.db.Exec(`DROP TRIGGER reject_transfer`); err != nil {
		t.Fatal(err)
	}
	transferred := manageAccess(t, s, device, a.Identity, access.Request{Action: "transfer", MembershipID: b.MembershipID})
	if transferred.Device.OwnershipID == device.OwnershipID || transferred.Device.OwnerTenant != b.TenantID || len(transferred.State.Shares) != 0 {
		t.Fatal("transfer retained prior scope")
	}
	if s.VerifyAccess(ctx, a.Identity, old, "info") == nil {
		t.Fatal("old owner retained access")
	}
	back := manageAccess(t, s, transferred.Device, b.Identity, access.Request{Action: "transfer", MembershipID: a.MembershipID})
	if back.Device.OwnershipID == device.OwnershipID || s.VerifyAccess(ctx, a.Identity, old, "info") == nil {
		t.Fatal("A-B-A transfer revived binding")
	}
	if _, err := s.ResolveAccess(ctx, back.Device, a.Identity); err != nil {
		t.Fatal(err)
	}
}
