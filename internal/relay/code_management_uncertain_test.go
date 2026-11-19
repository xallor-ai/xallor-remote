package relay

import (
	"context"
	"errors"
	"testing"

	"github.com/xallor-ai/xallor-remote/internal/access"
	"github.com/xallor-ai/xallor-remote/internal/accesscode"
	"github.com/xallor-ai/xallor-remote/internal/protocol"
)

type uncertainCodeManagementStore struct{ accesscode.Store }

func (s uncertainCodeManagementStore) WriteAccessCode(ctx context.Context, fn func(accesscode.Transaction) error) error {
	if err := s.Store.WriteAccessCode(ctx, fn); err != nil {
		return err
	}
	return errors.New("fixture committed code management but lost acknowledgement")
}

// An uncertain code revoke/rotate outcome quarantines the authenticated device
// route; continuing cached access would be unsafe because the DB may commit.
func TestCodeManagement_shouldQuarantineDevice_whenCommitOutcomeIsUncertain(t *testing.T) {
	s, h, device, _, _, ownerToken := typedCodeFixture(t)
	h.accessCodes = uncertainCodeManagementStore{Store: s.SQLStore}
	got := sendTypedAccess(t, h, device, ownerToken, access.Request{Action: "code.revoke", CodeID: "managed-code"})
	if got.Code != protocol.RelayError {
		t.Fatalf("uncertain revoke code=%s", got.Code)
	}
	if h.DeviceOnline("device") {
		t.Fatal("uncertain code management left route online")
	}
	if h.devices["device"].conn != nil || h.devices["device"].ready {
		t.Fatal("uncertain management did not clear device route")
	}
	if err := s.ReadAccessCode(context.Background(), func(r accesscode.Reader) error {
		code, err := r.Code(context.Background(), "managed-code")
		if err != nil {
			return err
		}
		if code.State != accesscode.StateRevoked {
			t.Error("committed revoke not durable")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
