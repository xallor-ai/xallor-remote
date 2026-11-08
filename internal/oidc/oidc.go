// Package oidc contains provider-neutral OIDC token verification primitives.
// It does not discover issuers, fetch JWKS, perform browser login, or map
// external subjects to local tenants. Those management concerns stay outside
// the Runtime and must supply an independently configured key set.
package oidc

import (
	"context"
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"math/big"
	"net/url"
	"strings"
	"sync"
	"time"
)

var (
	ErrInvalidConfig = errors.New("invalid OIDC verifier configuration")
	ErrUnauthorized  = errors.New("OIDC token is not authorized")
)

// Config is an explicit trust policy. Issuer and audience are exact matches;
// tenant/principal claims are intentionally absent from this contract.
type Config struct {
	Issuer   string
	Audience string
	JWKSURL  string
	Skew     time.Duration
}

func (c Config) Validate() error {
	if !httpsURL(c.Issuer) || strings.TrimSpace(c.Audience) == "" || !httpsURL(c.JWKSURL) || c.Skew < 0 || c.Skew > 10*time.Minute {
		return ErrInvalidConfig
	}
	return nil
}

// Claims contains only claims needed to establish an external subject. Local
// tenant, principal, role and membership IDs must be looked up separately.
type Claims struct {
	Issuer    string
	Subject   string
	Audience  []string
	ExpiresAt int64
	NotBefore *int64
	IssuedAt  *int64
}

// Subject is the only external identity key a management plane may map. It
// intentionally has no tenant, role or device fields.
type Subject struct {
	Issuer string
	Value  string
}

type MembershipRef struct {
	PrincipalID       string
	TenantID          string
	MembershipID      string
	MembershipVersion int64
}

// SubjectMapper is implemented by a management/account adapter, never by a
// token parser. A missing or inactive mapping must fail the login flow.
type SubjectMapper interface {
	Lookup(context.Context, Subject) (MembershipRef, error)
}

func (c Claims) SubjectKey() Subject { return Subject{Issuer: c.Issuer, Value: c.Subject} }

type tokenClaims struct {
	Issuer    string          `json:"iss"`
	Subject   string          `json:"sub"`
	Audience  json.RawMessage `json:"aud"`
	ExpiresAt *int64          `json:"exp"`
	NotBefore *int64          `json:"nbf"`
	IssuedAt  *int64          `json:"iat"`
}

func (c tokenClaims) claims() (Claims, error) {
	if c.Issuer == "" || c.Subject == "" || c.ExpiresAt == nil || *c.ExpiresAt <= 0 {
		return Claims{}, ErrUnauthorized
	}
	audience, err := parseAudience(c.Audience)
	if err != nil || len(audience) == 0 {
		return Claims{}, ErrUnauthorized
	}
	return Claims{Issuer: c.Issuer, Subject: c.Subject, Audience: audience, ExpiresAt: *c.ExpiresAt, NotBefore: c.NotBefore, IssuedAt: c.IssuedAt}, nil
}

// ValidateClaims applies issuer, audience and time checks after signature
// verification. now is injectable for deterministic tests and key rotation.
func ValidateClaims(c Config, claims Claims, now time.Time) error {
	if err := c.Validate(); err != nil || claims.Issuer != c.Issuer || claims.Subject == "" || claims.ExpiresAt <= 0 {
		return ErrUnauthorized
	}
	matched := false
	for _, audience := range claims.Audience {
		if audience == c.Audience {
			matched = true
			break
		}
	}
	if !matched {
		return ErrUnauthorized
	}
	stamp := now.Unix()
	if stamp >= claims.ExpiresAt+int64(c.Skew/time.Second) {
		return ErrUnauthorized
	}
	if claims.NotBefore != nil && stamp+int64(c.Skew/time.Second) < *claims.NotBefore {
		return ErrUnauthorized
	}
	if claims.IssuedAt != nil && *claims.IssuedAt > stamp+int64(c.Skew/time.Second) {
		return ErrUnauthorized
	}
	return nil
}

type Header struct {
	Algorithm string `json:"alg"`
	KeyID     string `json:"kid"`
	Type      string `json:"typ"`
}

type JWK struct {
	KeyID     string `json:"kid"`
	KeyType   string `json:"kty"`
	Use       string `json:"use,omitempty"`
	Algorithm string `json:"alg,omitempty"`
	N         string `json:"n"`
	E         string `json:"e"`
}

func (j JWK) RSA() (*rsa.PublicKey, error) {
	if j.KeyID == "" || j.KeyType != "RSA" || (j.Use != "" && j.Use != "sig") || (j.Algorithm != "" && j.Algorithm != "RS256") {
		return nil, ErrInvalidConfig
	}
	n, err := base64.RawURLEncoding.DecodeString(j.N)
	if err != nil || len(n) == 0 || new(big.Int).SetBytes(n).BitLen() < 2048 {
		return nil, ErrInvalidConfig
	}
	eBytes, err := base64.RawURLEncoding.DecodeString(j.E)
	if err != nil || len(eBytes) == 0 || len(eBytes) > 4 {
		return nil, ErrInvalidConfig
	}
	e := 0
	for _, b := range eBytes {
		e = e<<8 | int(b)
	}
	if e < 2 || e%2 == 0 {
		return nil, ErrInvalidConfig
	}
	return &rsa.PublicKey{N: new(big.Int).SetBytes(n), E: e}, nil
}

// KeySet is an atomically replaceable verifier key set. Fetching and
// authenticating JWKS metadata is deliberately owned by the management plane.
type KeySet struct {
	mu   sync.RWMutex
	keys map[string]*rsa.PublicKey
}

func NewKeySet(keys []JWK) (*KeySet, error) {
	k := &KeySet{}
	if err := k.Replace(keys); err != nil {
		return nil, err
	}
	return k, nil
}

func (k *KeySet) Replace(keys []JWK) error {
	if k == nil || len(keys) == 0 {
		return ErrInvalidConfig
	}
	next := make(map[string]*rsa.PublicKey, len(keys))
	for _, jwk := range keys {
		if _, exists := next[jwk.KeyID]; exists {
			return ErrInvalidConfig
		}
		key, err := jwk.RSA()
		if err != nil {
			return err
		}
		next[jwk.KeyID] = key
	}
	k.mu.Lock()
	k.keys = next
	k.mu.Unlock()
	return nil
}

func (k *KeySet) key(id string) *rsa.PublicKey {
	k.mu.RLock()
	defer k.mu.RUnlock()
	return k.keys[id]
}

// Verify verifies a compact JWT and returns only provider claims. It rejects
// alg=none, unknown algorithms/kids, malformed claims and invalid signatures.
func (k *KeySet) Verify(token string, cfg Config, now time.Time) (Claims, error) {
	if err := cfg.Validate(); err != nil || k == nil {
		return Claims{}, ErrUnauthorized
	}
	parts := strings.Split(token, ".")
	if len(parts) != 3 || parts[0] == "" || parts[1] == "" || parts[2] == "" {
		return Claims{}, ErrUnauthorized
	}
	var header Header
	headerBytes, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil || json.Unmarshal(headerBytes, &header) != nil || header.Algorithm != "RS256" || header.KeyID == "" {
		return Claims{}, ErrUnauthorized
	}
	key := k.key(header.KeyID)
	if key == nil {
		return Claims{}, ErrUnauthorized
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil || rsa.VerifyPKCS1v15(key, crypto.SHA256, digest(parts[0]+"."+parts[1]), signature) != nil {
		return Claims{}, ErrUnauthorized
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return Claims{}, ErrUnauthorized
	}
	var raw tokenClaims
	if json.Unmarshal(payload, &raw) != nil {
		return Claims{}, ErrUnauthorized
	}
	claims, err := raw.claims()
	if err != nil || ValidateClaims(cfg, claims, now) != nil {
		return Claims{}, ErrUnauthorized
	}
	return claims, nil
}

func digest(value string) []byte {
	sum := sha256.Sum256([]byte(value))
	return sum[:]
}

func parseAudience(raw json.RawMessage) ([]string, error) {
	if len(raw) == 0 {
		return nil, ErrUnauthorized
	}
	if raw[0] == '"' {
		var value string
		if json.Unmarshal(raw, &value) != nil || value == "" {
			return nil, ErrUnauthorized
		}
		return []string{value}, nil
	}
	var values []string
	if json.Unmarshal(raw, &values) != nil {
		return nil, ErrUnauthorized
	}
	for _, value := range values {
		if value == "" {
			return nil, ErrUnauthorized
		}
	}
	return values, nil
}

func httpsURL(value string) bool {
	u, err := url.Parse(value)
	return err == nil && u.Scheme == "https" && u.Host != "" && u.User == nil && u.RawQuery == "" && u.Fragment == ""
}
