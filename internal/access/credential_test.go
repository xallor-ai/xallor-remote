package access

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/xallor-ai/xallor-remote/internal/principal"
)

type credentialStore struct {
	*accessMemory
	read func() principal.Credential
}

type credentialTransaction struct {
	Transaction
	read func() principal.Credential
}

func (t credentialTransaction) CredentialByID(context.Context, string) (principal.Credential, error) {
	return t.read(), nil
}
func (s credentialStore) ReadAccess(ctx context.Context, fn func(Reader) error) error {
	return s.accessMemory.ReadAccess(ctx, func(Reader) error { return fn(credentialTransaction{&s.data, s.read}) })
}
func (s credentialStore) WriteAccess(ctx context.Context, fn func(Transaction) error) error {
	return s.accessMemory.WriteAccess(ctx, func(tx Transaction) error { return fn(credentialTransaction{tx, s.read}) })
}

// Access must apply principal policy within its own transaction even when the
// reader returns revoked/inactive data or time advances during the read.
func TestService_should_revalidateCredentialPolicy_inEveryAccessUseCase(t *testing.T) {
	for _, operation := range []string{"resolve", "verify", "manage"} {
		for _, condition := range []string{"revoked", "inactive", "expired-during-read"} {
			t.Run(operation+"/"+condition, func(t *testing.T) {
				s, memory, actor := serviceFixture()
				now := int64(1000)
				s.now = func() time.Time { return time.Unix(now, 0) }
				s.store = credentialStore{memory, func() principal.Credential {
					row := principal.Credential{Identity: actor, MembershipActive: true}
					switch condition {
					case "revoked":
						row.Revoked = true
					case "inactive":
						row.MembershipActive = false
					case "expired-during-read":
						now = actor.ExpiresAt
					}
					return row
				}}
				device := memory.data.device
				ctx := context.Background()
				var err error
				switch operation {
				case "resolve":
					_, err = s.Resolve(ctx, device, actor)
				case "verify":
					err = s.Verify(ctx, actor, Authorization{Binding: device.Binding(), Permissions: AllPermissions()}, "info")
				case "manage":
					_, err = s.Manage(ctx, "device", device, actor, Request{Action: "transfer", MembershipID: "guest"})
				}
				if !errors.Is(err, ErrDenied) || memory.data.device != device {
					t.Fatal("invalid actor was authorized or changed ownership", err)
				}
			})
		}
	}
}
