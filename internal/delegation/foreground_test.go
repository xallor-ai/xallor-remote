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

// Live foreground routes have no handoff proof age, but the original identity,
// device binding, permission and expiry are still rechecked on every renewal.
func TestForeground_should_renewLongRunningRoute_withoutExtendingExpiredAuthority(t *testing.T) {
	a, f, _ := authorityForTest(t)
	now := time.Unix(1000, 0)
	a.now = func() time.Time { return now }
	f.operation = protocol.OpExec
	f.device.OwnershipID = strings.Repeat("e", 32)
	f.identity = principal.Identity{PrincipalID: "subject", ExpiresAt: 10000}
	f.authorization = access.Authorization{Binding: f.device.Binding(), ShareID: "original", ExpiresAt: 9000, Permissions: access.AllPermissions()}
	invocation := Invocation{DeviceID: "123456789", Binding: f.device.Binding(), Identity: f.identity, Authorization: f.authorization}
	renew := func(want time.Duration, denied bool) {
		t.Helper()
		ttl, err := a.RenewForeground(context.Background(), invocation)
		if denied {
			if !errors.Is(err, ErrDenied) {
				t.Fatal("expired or changed authority renewed", err)
			}
		} else if err != nil || ttl != want {
			t.Fatal(ttl, err)
		}
	}
	renew(protocol.ExecutionLeaseDuration, false)
	now = now.Add(2 * time.Hour)
	renew(protocol.ExecutionLeaseDuration, false)
	now = time.Unix(8999, 0)
	renew(time.Second, false)
	f.after = func() { now = now.Add(2 * time.Second) }
	renew(0, true)
	f.after = nil
	now = time.Unix(2000, 0)
	f.authorization.ShareID = "replacement"
	renew(0, true)
	f.authorization = invocation.Authorization
	f.identity.MembershipVersion++
	renew(0, true)
}

// Anonymous authority is independent of accounts; a changed device instance or
// grant remains a hard denial. Storage failure must not become permission.
func TestForeground_should_rejectChangedBinding_andPreserveAnonymousAuthority(t *testing.T) {
	a, f, _ := authorityForTest(t)
	invocation := Invocation{DeviceID: "123456789", Binding: f.device.Binding()}
	f.err = principal.ErrUnauthorized
	if _, err := a.RenewForeground(context.Background(), invocation); err != nil {
		t.Fatal(err)
	}
	f.device.GrantID = strings.Repeat("d", 32)
	if _, err := a.RenewForeground(context.Background(), invocation); !errors.Is(err, ErrDenied) {
		t.Fatal(err)
	}
}
