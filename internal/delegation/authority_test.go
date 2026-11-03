package delegation

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/xallor-ai/xallor-remote/internal/access"
	"github.com/xallor-ai/xallor-remote/internal/principal"
	"github.com/xallor-ai/xallor-remote/internal/protocol"
)

type authorityFixture struct {
	device        access.Device
	identity      principal.Identity
	authorization access.Authorization
	err           error
	after         func()
}

func (f *authorityFixture) Authority() string { return strings.Repeat("a", 32) }
func (f *authorityFixture) LoadDevice(context.Context, string) (access.Device, error) {
	return f.device, nil
}
func (f *authorityFixture) Authenticate(context.Context, string) (principal.Identity, error) {
	panic("not used")
}
func (f *authorityFixture) Verify(_ context.Context, p principal.Identity) error {
	if f.after != nil {
		f.after()
	}
	if p != f.identity {
		return principal.ErrUnauthorized
	}
	return f.err
}

type permissionsFixture struct{ *authorityFixture }

func (f permissionsFixture) Verify(_ context.Context, p principal.Identity, a access.Authorization, op string) error {
	if f.after != nil {
		f.after()
	}
	if p != f.identity || a != f.authorization || op != protocol.OpHandoffSubmit {
		return access.ErrDenied
	}
	return f.err
}
func authorityForTest(t *testing.T) (*Authority, *authorityFixture, string) {
	t.Helper()
	f := &authorityFixture{device: access.Device{UID: strings.Repeat("b", 32), GrantID: strings.Repeat("c", 32), Inbound: true}}
	a := NewAuthority([32]byte{1}, f, f, permissionsFixture{f})
	proof, err := a.Issue("123456789", f.device.Binding(), f.identity, f.authorization)
	if err != nil {
		t.Fatal(err)
	}
	return a, f, proof
}

// The issuer must never forward a proof its own verifier would reject on size.
func TestAuthority_should_rejectOversizeProof_beforeIssuing(t *testing.T) {
	a, f, _ := authorityForTest(t)
	proof, err := a.Issue(strings.Repeat("x", protocol.MaxDelegationBytes), f.device.Binding(), f.identity, f.authorization)
	if !errors.Is(err, ErrDenied) || proof != "" {
		t.Fatal("oversized proof issued")
	}
}

// A signed delegation is restricted to the issuing authority, device instance,
// original access binding and bounded age; none can be changed by a caller.
func TestAuthority_should_denyTamperingWrongDeviceOrChangedBinding(t *testing.T) {
	for _, kind := range []string{"tampered", "device", "uid", "grant", "inbound", "key", "expired"} {
		t.Run(kind, func(t *testing.T) {
			a, f, proof := authorityForTest(t)
			id, uid := "123456789", f.device.UID
			if ttl, err := a.Renew(context.Background(), id, uid, proof); err != nil || ttl != protocol.ExecutionLeaseDuration {
				t.Fatal(ttl, err)
			}
			switch kind {
			case "tampered":
				proof = "A" + proof[1:]
			case "device":
				id = "other"
			case "uid":
				uid = "other"
			case "grant":
				f.device.GrantID = strings.Repeat("d", 32)
			case "inbound":
				f.device.Inbound = false
			case "key":
				a.key = [32]byte{2}
			case "expired":
				a.now = func() time.Time { return time.Now().Add(protocol.DelegationProofLifetime + time.Second) }
			}
			if _, err := a.Renew(context.Background(), id, uid, proof); !errors.Is(err, ErrDenied) {
				t.Fatal(err)
			}
		})
	}
}

// Credential/member and share validation uses the original signed snapshot.
// Storage failure stays distinct; a read that crosses expiry cannot renew.
func TestAuthority_should_revalidateOriginalPrincipalAndShare_afterStorageWait(t *testing.T) {
	for _, owned := range []bool{false, true} {
		t.Run(map[bool]string{false: "unclaimed", true: "owned"}[owned], func(t *testing.T) {
			a, f, _ := authorityForTest(t)
			now := time.Unix(1000, 0)
			a.now = func() time.Time { return now }
			f.identity = principal.Identity{PrincipalID: "subject", ExpiresAt: 1002}
			if owned {
				f.device.OwnershipID = strings.Repeat("e", 32)
				f.authorization = access.Authorization{Binding: f.device.Binding(), ShareID: "share", ExpiresAt: 1001, Permissions: access.AllPermissions()}
			}
			proof, err := a.Issue("123456789", f.device.Binding(), f.identity, f.authorization)
			if err != nil {
				t.Fatal(err)
			}
			ttl, err := a.Renew(context.Background(), "123456789", f.device.UID, proof)
			if err != nil || ttl > 2*time.Second {
				t.Fatal(ttl, err)
			}
			f.err = errors.New("storage unavailable")
			if _, err := a.Renew(context.Background(), "123456789", f.device.UID, proof); err != f.err {
				t.Fatal("storage failure concealed", err)
			}
			f.err = principal.ErrUnauthorized
			if _, err := a.Renew(context.Background(), "123456789", f.device.UID, proof); !errors.Is(err, ErrDenied) {
				t.Fatal(err)
			}
			f.err = nil
			f.after = func() { now = now.Add(3 * time.Second) }
			if _, err := a.Renew(context.Background(), "123456789", f.device.UID, proof); !errors.Is(err, ErrDenied) {
				t.Fatal("late renewal accepted", err)
			}
		})
	}
}
