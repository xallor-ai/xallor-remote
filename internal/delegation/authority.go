// Package delegation separates renewable execution authority from stable
// receipt identity. Proofs never contain bearer credentials or command content.
package delegation

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/xallor-ai/xallor-remote/internal/access"
	"github.com/xallor-ai/xallor-remote/internal/accesscode"
	"github.com/xallor-ai/xallor-remote/internal/principal"
	"github.com/xallor-ai/xallor-remote/internal/protocol"
)

var ErrDenied = errors.New("execution authorization denied")
var ErrExpired = errors.New("execution authorization expired")

type Devices interface {
	Authority() string
	LoadDevice(context.Context, string) (access.Device, error)
}
type Access interface {
	Verify(context.Context, principal.Identity, access.Authorization, string) error
}

type Authority struct {
	key      [32]byte
	devices  Devices
	accounts principal.Authenticator
	access   Access
	now      func() time.Time
	sessions accesscode.Store
}

func (a *Authority) SetSessionStore(store accesscode.Store) { a.sessions = store }

func NewAuthority(key [32]byte, devices Devices, accounts principal.Authenticator, permissions Access) *Authority {
	return &Authority{key: key, devices: devices, accounts: accounts, access: permissions, now: time.Now}
}

type claims struct {
	Authority     string
	DeviceID      string
	Binding       access.Binding
	Identity      principal.Identity
	Authorization access.Authorization
	Expires       int64
	SessionID     string
	CodeID        string
	Generation    uint64
	TargetEpoch   string
	SessionScope  []string
}

// Issue accepts only the transport's already authenticated connection snapshot.
// A proof permits revalidation, never execution without a fresh renewal.
func (a *Authority) Issue(deviceID string, binding access.Binding, identity principal.Identity, authorization access.Authorization) (string, error) {
	if a.devices == nil || !binding.Matches(binding) || a.key == [32]byte{} {
		return "", ErrDenied
	}
	c := claims{Authority: a.devices.Authority(), DeviceID: deviceID, Binding: binding, Identity: identity, Authorization: authorization, Expires: a.now().Add(protocol.DelegationProofLifetime).UnixMilli()}
	return a.issue(c)
}

// IssueSession binds a background proof to the approved logical session.
func (a *Authority) IssueSession(deviceID string, binding access.Binding, identity principal.Identity, authorization access.Authorization, session accesscode.SessionAuthorization) (string, error) {
	if a.devices == nil || a.key == [32]byte{} || !binding.Matches(binding) || !session.Target.Valid() || session.Target.DeviceUID != binding.DeviceUID || session.CanOperate(a.now().UTC()) != nil || !session.Allows(protocol.OpHandoffSubmit) {
		return "", ErrDenied
	}
	expires := a.now().Add(protocol.DelegationProofLifetime).UnixMilli()
	if session.ExpiresAt != nil {
		expires = min(expires, session.ExpiresAt.UnixMilli())
	}
	return a.issue(claims{Authority: a.devices.Authority(), DeviceID: deviceID, Binding: binding, Identity: identity, Authorization: authorization, Expires: expires, SessionID: session.ID, CodeID: session.CodeID, Generation: session.Generation, TargetEpoch: session.Target.Epoch, SessionScope: append([]string(nil), session.Scope...)})
}

func (a *Authority) issue(c claims) (string, error) {
	data, err := json.Marshal(c)
	if err != nil {
		return "", err
	}
	mac := hmac.New(sha256.New, a.key[:])
	mac.Write(data)
	proof := base64.RawURLEncoding.EncodeToString(data) + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	if len(proof) > protocol.MaxDelegationBytes {
		return "", ErrDenied
	}
	return proof, nil
}

// Renew rechecks the signed caller and original grant against current storage.
// Device proof comes from the authenticated device connection, never the token.
func (a *Authority) Renew(ctx context.Context, deviceID, uid, proof string) (time.Duration, error) {
	if len(proof) == 0 || len(proof) > protocol.MaxDelegationBytes || a.devices == nil || a.key == [32]byte{} {
		return 0, ErrDenied
	}
	parts := strings.Split(proof, ".")
	if len(parts) != 2 {
		return 0, ErrDenied
	}
	data, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return 0, ErrDenied
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return 0, ErrDenied
	}
	mac := hmac.New(sha256.New, a.key[:])
	mac.Write(data)
	if !hmac.Equal(signature, mac.Sum(nil)) {
		return 0, ErrDenied
	}
	var c claims
	if json.Unmarshal(data, &c) != nil || c.Authority != a.devices.Authority() || c.DeviceID != deviceID || c.Binding.DeviceUID != uid || c.Expires <= a.now().UnixMilli() {
		return 0, ErrDenied
	}
	if c.SessionID != "" && a.sessions != nil {
		var session accesscode.SessionAuthorization
		if err := a.sessions.ReadAccessCode(ctx, func(r accesscode.Reader) error { var err error; session, err = r.Session(ctx, c.SessionID); return err }); err != nil {
			return 0, err
		}
		if session.ID != c.SessionID || session.CodeID != c.CodeID || session.Generation != c.Generation || !session.Target.Valid() || session.Target.DeviceUID != uid || session.Target.Epoch != c.TargetEpoch || session.CanOperate(a.now().UTC()) != nil || !session.Allows(protocol.OpHandoffSubmit) || !slices.Equal(session.Scope, c.SessionScope) {
			return 0, ErrDenied
		}
		// Preserve both the signed original deadline and any tighter current
		// consent deadline through the remaining identity/permission I/O.
		if session.ExpiresAt != nil {
			c.Expires = min(c.Expires, session.ExpiresAt.UnixMilli())
		}
	} else if c.SessionID != "" {
		return 0, ErrDenied
	}
	return a.renew(ctx, c, protocol.OpHandoffSubmit)
}

// Invocation is a trusted snapshot retained by the Relay's live route. It is
// never decoded from a renewal request or supplied by the controlling client.
type Invocation struct {
	DeviceID      string
	Binding       access.Binding
	Identity      principal.Identity
	Authorization access.Authorization
}

// RenewForeground uses the live route instead of a time-limited proof. The
// router must verify the requesting device and retain the same route across I/O.
func (a *Authority) RenewForeground(ctx context.Context, invocation Invocation) (time.Duration, error) {
	return a.renew(ctx, claims{DeviceID: invocation.DeviceID, Binding: invocation.Binding,
		Identity: invocation.Identity, Authorization: invocation.Authorization}, protocol.OpExec)
}

func (a *Authority) renew(ctx context.Context, c claims, operation string) (time.Duration, error) {
	if a.devices == nil || !c.Binding.Matches(c.Binding) {
		return 0, ErrDenied
	}
	device, err := a.devices.LoadDevice(ctx, c.DeviceID)
	if errors.Is(err, access.ErrDeviceNotFound) {
		return 0, ErrDenied
	}
	if err != nil {
		return 0, err
	}
	if !device.Inbound || !c.Binding.Matches(device.Binding()) {
		return 0, ErrDenied
	}
	if c.Binding.OwnershipID != "" {
		if a.access == nil {
			return 0, ErrDenied
		}
		err = a.access.Verify(ctx, c.Identity, c.Authorization, operation)
	} else if c.Identity.PrincipalID != "" {
		if a.accounts == nil {
			return 0, ErrDenied
		}
		err = a.accounts.Verify(ctx, c.Identity)
	}
	if errors.Is(err, access.ErrDenied) || errors.Is(err, principal.ErrUnauthorized) {
		return 0, ErrDenied
	}
	if err != nil {
		return 0, err
	}
	if err = ctx.Err(); err != nil {
		return 0, err
	}
	// Evaluate after all reads, and do not renew beyond any signed expiry.
	now := a.now()
	expires := now.Add(protocol.ExecutionLeaseDuration)
	if c.Expires > 0 && time.UnixMilli(c.Expires).Before(expires) {
		expires = time.UnixMilli(c.Expires)
	}
	for _, seconds := range []int64{c.Identity.ExpiresAt, c.Authorization.ExpiresAt} {
		if seconds > 0 && time.Unix(seconds, 0).Before(expires) {
			expires = time.Unix(seconds, 0)
		}
	}
	ttl := expires.Sub(now)
	if ttl <= 0 {
		return 0, ErrDenied
	}
	return ttl, nil
}
