package accesscode

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math"
	"strings"
	"time"
	"unicode/utf8"
)

var ErrRotationConflict = errors.New("access-code rotation intent conflicts")
var ErrRotationNotFound = errors.New("access-code rotation is not recorded")

type Rotation struct {
	PreviousCodeID    string
	ReplacementCodeID string
	IntentSHA256      string
}

func RotationDigest(previous, replacement, secretHash string) (string, error) {
	for _, id := range []string{previous, replacement} {
		if id == "" || len(id) > 128 || strings.TrimSpace(id) != id || !utf8.ValidString(id) {
			return "", ErrInvalid
		}
	}
	if len(secretHash) != 64 {
		return "", ErrInvalid
	}
	decoded, err := hex.DecodeString(secretHash)
	if previous == replacement || err != nil || len(decoded) != 32 || secretHash != strings.ToLower(secretHash) {
		return "", ErrInvalid
	}
	raw, err := json.Marshal(struct{ Domain, Previous, Replacement, SecretSHA256 string }{"xallor.remote.code-rotation/v1", previous, replacement, secretHash})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

// Replacement preserves authorization policy and deadline. It cannot turn an
// expired or consumed code into fresh eligibility; one-time use history is
// additionally checked by the storage transaction before this transition.
func (c Code) Replacement(id string, generation uint64, now time.Time) (Code, error) {
	if now.IsZero() || now.Before(c.IssuedAt) || id == "" || id == c.ID || generation <= c.Generation || generation > math.MaxInt64 || c.DeviceUID == "" {
		return Code{}, ErrInvalid
	}
	if err := c.redemptionError(now); err != nil {
		return Code{}, err
	}
	scope, err := c.ScopeForSession(c.Generation, c.Scope)
	if err != nil {
		return Code{}, err
	}
	next := c
	next.ID, next.Generation, next.IssuedAt, next.State = id, generation, time.Unix(now.Unix(), 0).UTC(), StateActive
	next.Scope = scope
	if c.ExpiresAt != nil {
		expires := c.ExpiresAt.UTC()
		next.ExpiresAt = &expires
	}
	if err := next.Validate(); err != nil {
		return Code{}, err
	}
	return next, nil
}
