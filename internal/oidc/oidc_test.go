package oidc

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"math/big"
	"strings"
	"testing"
	"time"
)

func testConfig() Config {
	return Config{Issuer: "https://issuer.example", Audience: "xallor", JWKSURL: "https://issuer.example/keys", Skew: time.Minute}
}

func jwk(id string, key *rsa.PrivateKey) JWK {
	return JWK{KeyID: id, KeyType: "RSA", Use: "sig", Algorithm: "RS256", N: base64.RawURLEncoding.EncodeToString(key.N.Bytes()), E: base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.E)).Bytes())}
}

func token(t *testing.T, key *rsa.PrivateKey, kid string, claims map[string]any, alg string) string {
	t.Helper()
	header, _ := json.Marshal(map[string]string{"alg": alg, "kid": kid, "typ": "JWT"})
	payload, _ := json.Marshal(claims)
	encode := func(value []byte) string { return base64.RawURLEncoding.EncodeToString(value) }
	input := encode(header) + "." + encode(payload)
	sum := digest(input)
	signature, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, sum)
	if err != nil {
		t.Fatal(err)
	}
	return input + "." + encode(signature)
}

// Provider-neutral verification accepts only the configured issuer/audience
// and external subject; tenant/principal claims never become local identity.
func TestVerify_should_failClosedOnIssuerAudienceAndTimeClaims(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	keys, err := NewKeySet([]JWK{jwk("one", key)})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(2_000, 0)
	base := map[string]any{"iss": "https://issuer.example", "sub": "subject-1", "aud": "xallor", "exp": now.Add(time.Hour).Unix(), "iat": now.Unix(), "tenant_id": "attacker-tenant", "principal_id": "attacker-principal"}
	claims, err := keys.Verify(token(t, key, "one", base, "RS256"), testConfig(), now)
	if err != nil || claims.Subject != "subject-1" || claims.Issuer != testConfig().Issuer {
		t.Fatalf("valid claims rejected: %+v %v", claims, err)
	}
	for name, mutate := range map[string]func(map[string]any){
		"wrong issuer":    func(c map[string]any) { c["iss"] = "https://other.example" },
		"wrong audience":  func(c map[string]any) { c["aud"] = "other" },
		"expired":         func(c map[string]any) { c["exp"] = now.Add(-time.Hour).Unix() },
		"not before":      func(c map[string]any) { c["nbf"] = now.Add(time.Hour).Unix() },
		"missing subject": func(c map[string]any) { delete(c, "sub") },
		"wrong key":       func(c map[string]any) { c["kid"] = "unused" },
	} {
		t.Run(name, func(t *testing.T) {
			copy := map[string]any{}
			for k, v := range base {
				copy[k] = v
			}
			mutate(copy)
			if name == "wrong key" {
				if _, err := keys.Verify(token(t, key, "unknown", copy, "RS256"), testConfig(), now); !errors.Is(err, ErrUnauthorized) {
					t.Fatal(err)
				}
				return
			}
			if _, err := keys.Verify(token(t, key, "one", copy, "RS256"), testConfig(), now); !errors.Is(err, ErrUnauthorized) {
				t.Fatal(err)
			}
		})
	}
	if _, err := keys.Verify(token(t, key, "one", base, "none"), testConfig(), now); !errors.Is(err, ErrUnauthorized) {
		t.Fatal(err)
	}
}

// Key replacement is atomic: a rotated set accepts the new kid and rejects
// the retired key, without network discovery or a fallback trust root.
func TestKeySet_should_rotateKeysWithoutAcceptingRetiredKey(t *testing.T) {
	one, _ := rsa.GenerateKey(rand.Reader, 2048)
	two, _ := rsa.GenerateKey(rand.Reader, 2048)
	keys, err := NewKeySet([]JWK{jwk("one", one)})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(2_000, 0)
	claims := map[string]any{"iss": testConfig().Issuer, "sub": "subject", "aud": []string{"xallor"}, "exp": now.Add(time.Hour).Unix()}
	if _, err := keys.Verify(token(t, one, "one", claims, "RS256"), testConfig(), now); err != nil {
		t.Fatal(err)
	}
	if err := keys.Replace([]JWK{jwk("two", two)}); err != nil {
		t.Fatal(err)
	}
	if _, err := keys.Verify(token(t, one, "one", claims, "RS256"), testConfig(), now); !errors.Is(err, ErrUnauthorized) {
		t.Fatal(err)
	}
	if _, err := keys.Verify(token(t, two, "two", claims, "RS256"), testConfig(), now); err != nil {
		t.Fatal(err)
	}
}

func TestConfig_should_requireExplicitHTTPSIssuerAudienceAndJWKS(t *testing.T) {
	for _, cfg := range []Config{
		{Issuer: "http://issuer.example", Audience: "xallor", JWKSURL: "https://issuer.example/keys"},
		{Issuer: "https://issuer.example", Audience: "", JWKSURL: "https://issuer.example/keys"},
		{Issuer: "https://issuer.example", Audience: "xallor", JWKSURL: "http://issuer.example/keys"},
		{Issuer: "https://issuer.example?tenant=bad", Audience: "xallor", JWKSURL: "https://issuer.example/keys"},
	} {
		if !errors.Is(cfg.Validate(), ErrInvalidConfig) {
			t.Fatalf("accepted config: %+v", cfg)
		}
	}
	if strings.Contains(string(mustJSON(Claims{Subject: "subject"})), "tenant") {
		t.Fatal("claims contract grew tenant authority")
	}
}

func mustJSON(value any) []byte { raw, _ := json.Marshal(value); return raw }
