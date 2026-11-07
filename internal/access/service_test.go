package access

import (
	"context"
	"errors"
	"strconv"
	"testing"

	"github.com/xallor-ai/xallor-remote/internal/protocol"
)

// Connection proof and the actor are rechecked in the same transaction before
// any mutation. Neither a forged tenant nor a stale device incarnation suffices.
func TestService_should_rejectMutation_whenActorOrDeviceProofChanged(t *testing.T) {
	for _, kind := range []string{"actor", "uid", "secret", "ownership", "non-owner"} {
		t.Run(kind, func(t *testing.T) {
			s, m, actor := serviceFixture()
			expected := m.data.device
			switch kind {
			case "actor":
				actor.TenantID = "forged"
			case "uid":
				expected.UID = "previous-device"
			case "secret":
				expected.SecretHash = "wrong-proof"
			case "ownership":
				expected.OwnershipID = "previous-ownership"
			case "non-owner":
				m.data.device.OwnerPrincipal = "somebody-else"
			}
			before := m.data.device
			_, err := s.Manage(context.Background(), "device", expected, actor, Request{Action: "transfer", MembershipID: "guest"})
			if !errors.Is(err, ErrDenied) || m.data.device != before {
				t.Fatal("untrusted mutation", err)
			}
		})
	}
}

// Repeating a claim keeps ownership stable; a real transfer gets a new identity
// and removes old shares. An inactive recipient cannot receive ownership.
func TestService_should_preserveClaimIdentity_and_replaceItOnTransfer(t *testing.T) {
	s, m, actor := serviceFixture()
	ctx := context.Background()
	before := m.data.device
	claim, err := s.Manage(ctx, "device", before, actor, Request{Action: "claim"})
	if err != nil || claim.Device != before {
		t.Fatal("claim changed owner", err)
	}
	m.data.shares["old"] = ShareRecord{ID: "old", OwnershipID: before.OwnershipID}
	member := m.data.members["guest"]
	member.Active = false
	m.data.members["guest"] = member
	if _, err := s.Manage(ctx, "device", before, actor, Request{Action: "transfer", MembershipID: "guest"}); !errors.Is(err, ErrDenied) {
		t.Fatal(err)
	}
	member.Active = true
	m.data.members["guest"] = member
	transfer, err := s.Manage(ctx, "device", before, actor, Request{Action: "transfer", MembershipID: "guest"})
	if err != nil || transfer.Device.OwnershipID == before.OwnershipID || transfer.Device.OwnerTenant != member.TenantID || transfer.Device.OwnerPrincipal != member.PrincipalID || len(transfer.State.Shares) != 0 {
		t.Fatal("transfer did not replace ownership", err)
	}
}

// The configured share ceiling applies to active inventory. Expired shares
// can be reclaimed, and replacement creates a new grant without using a slot.
func TestService_should_enforceShareLimit_and_replaceExpiredOrExistingShares(t *testing.T) {
	s, m, actor := serviceFixture()
	ctx := context.Background()
	device := m.data.device
	for i := range protocol.MaxDeviceShares {
		id := strconv.Itoa(i)
		m.data.shares[id] = ShareRecord{ID: id, OwnershipID: device.OwnershipID, MembershipID: "member" + id, ExpiresAt: 2000}
	}
	req := Request{Action: "share", MembershipID: "guest", Operations: []string{protocol.OpRead}, TTLSeconds: 60}
	if _, err := s.Manage(ctx, "device", device, actor, req); !errors.Is(err, ErrLimit) || len(m.data.shares) != protocol.MaxDeviceShares {
		t.Fatal("share ceiling bypassed", err)
	}
	expired := m.data.shares["0"]
	expired.ExpiresAt = 1000
	m.data.shares["0"] = expired
	if _, err := s.Manage(ctx, "device", device, actor, req); err != nil {
		t.Fatal(err)
	}
	first, _ := m.data.Share(ctx, device.OwnershipID, "guest")
	if first.ExpiresAt != 1060 || first.Permissions != 2 || len(m.data.shares) != protocol.MaxDeviceShares {
		t.Fatal("incorrect grant")
	}
	if _, err := s.Manage(ctx, "device", device, actor, req); err != nil {
		t.Fatal(err)
	}
	next, _ := m.data.Share(ctx, device.OwnershipID, "guest")
	if first.ID == next.ID || len(m.data.shares) != protocol.MaxDeviceShares {
		t.Fatal("replacement revived old authorization")
	}
}

// Authorization uses stored ownership, not caller-provided owner fields. The
// share expiry boundary and permission set apply even to authenticated users.
func TestService_should_useStoredOwner_and_enforceShareExpiryAndOperations(t *testing.T) {
	s, m, actor := serviceFixture()
	ctx := context.Background()
	m.data.device.OwnerPrincipal = "real-owner"
	forged := m.data.device
	forged.OwnerPrincipal = actor.PrincipalID
	if _, err := s.Resolve(ctx, forged, actor); !errors.Is(err, ErrDenied) {
		t.Fatal("forged owner accepted", err)
	}
	m.data.shares["grant"] = ShareRecord{ID: "grant", OwnershipID: forged.OwnershipID, MembershipID: actor.MembershipID, Permissions: 2, ExpiresAt: 1001}
	auth, err := s.Resolve(ctx, forged, actor)
	if err != nil || s.Verify(ctx, actor, auth, protocol.OpRead) != nil {
		t.Fatal("valid read denied", err)
	}
	if !errors.Is(s.Verify(ctx, actor, auth, protocol.OpExec), ErrDenied) {
		t.Fatal("read capability implied exec")
	}
	grant := m.data.shares["grant"]
	grant.ExpiresAt = 1000
	m.data.shares["grant"] = grant
	if _, err := s.Resolve(ctx, forged, actor); !errors.Is(err, ErrDenied) {
		t.Fatal("expired grant accepted", err)
	}
}

// An ambiguous committed mutation must not be published as success or retried
// by the application, even when storage has changed the owner already.
func TestService_should_returnNoMutation_whenCommitOutcomeUnconfirmed(t *testing.T) {
	s, m, actor := serviceFixture()
	m.commitErr = lostCommit
	result, err := s.Manage(context.Background(), "device", m.data.device, actor, Request{Action: "transfer", MembershipID: "guest"})
	if !errors.Is(err, lostCommit) || result.Device.UID != "" || result.State.DeviceUID != "" || m.writes != 1 {
		t.Fatal("uncertain commit published or replayed", err)
	}
}
