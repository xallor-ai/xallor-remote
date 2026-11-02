// Package principal defines trusted caller identity, independently of device
// access grants and subscription entitlements.
package principal

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/xallor-ai/xallor-remote/internal/protocol"
)

var ErrUnauthorized = errors.New("principal credential is not authorized")

// Identity is derived by the authority, never from caller-supplied IDs.
type Identity struct {
	Authority         string `json:"authority"`
	PrincipalID       string `json:"principal_id"`
	TenantID          string `json:"tenant_id"`
	MembershipID      string `json:"membership_id"`
	MembershipVersion int64  `json:"membership_version"`
	CredentialID      string `json:"credential_id"`
	ExpiresAt         int64  `json:"expires_at"`
}

type Authenticator interface {
	Authenticate(context.Context, string) (Identity, error)
	Verify(context.Context, Identity) error
}

type Enrollment struct {
	Identity
	Token string `json:"token"`
}

func NewToken() (string, error) {
	raw, err := protocol.RandomHex(32)
	return "xr_account_" + raw, err
}

func ValidToken(token string) bool {
	if !strings.HasPrefix(token, "xr_account_") || len(token) != len("xr_account_")+64 {
		return false
	}
	for _, c := range strings.TrimPrefix(token, "xr_account_") {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

func ValidateEnrollment(name, kind string, ttl time.Duration) error {
	if strings.TrimSpace(name) == "" || len(name) > 200 || (kind != "user" && kind != "service") || ttl < time.Minute || ttl > 90*24*time.Hour {
		return errors.New("需要名称、user/service 类型和 1 分钟至 90 天的有效期")
	}
	return nil
}
