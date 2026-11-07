package relay

import (
	"context"
	"testing"
	"time"

	"github.com/xallor-ai/xallor-remote/internal/access"
	"github.com/xallor-ai/xallor-remote/internal/account"
	"github.com/xallor-ai/xallor-remote/internal/principal"
)

func enrollment(t *testing.T, s account.Store, name, tenant string) principal.Enrollment {
	t.Helper()
	p, err := account.New(s).CreatePrincipal(context.Background(), account.CreatePrincipalRequest{Name: name, Kind: "user", TenantID: tenant, TTL: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func manageAccess(t *testing.T, s access.Store, device access.Device, p principal.Identity, req access.Request) access.Mutation {
	t.Helper()
	r, err := access.NewService(s).Manage(context.Background(), "device", device, p, req)
	if err != nil {
		t.Fatal(req.Action, err)
	}
	return r
}
