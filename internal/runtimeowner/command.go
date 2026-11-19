package runtimeowner

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"path/filepath"
	"strings"
)

type Command struct {
	ID     string `json:"operation_id"`
	Kind   string `json:"kind"`
	Bundle string `json:"bundle,omitempty"`
}

func NewCommand(kind, bundle string) (Command, error) {
	if kind == "install" {
		if bundle == "" {
			return Command{}, ErrInvalid
		}
		absolute, err := filepath.Abs(bundle)
		if err != nil {
			return Command{}, err
		}
		bundle = absolute
	}
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return Command{}, err
	}
	c := Command{ID: hex.EncodeToString(id[:]), Kind: kind, Bundle: bundle}
	_, err := c.Request()
	return c, err
}

// Request derives the digest from the actual validated command. Canonical
// absolute input keeps its meaning stable across owner/worker working directories.
func (c Command) Request() (Request, error) {
	if !idPattern.MatchString(c.ID) || len(c.Bundle) > 32768 || strings.ContainsRune(c.Bundle, 0) {
		return Request{}, ErrInvalid
	}
	switch c.Kind {
	case "install":
		if !filepath.IsAbs(c.Bundle) || filepath.Clean(c.Bundle) != c.Bundle {
			return Request{}, ErrInvalid
		}
	case "rollback":
		if c.Bundle != "" {
			return Request{}, ErrInvalid
		}
	default:
		return Request{}, ErrInvalid
	}
	raw, _ := json.Marshal(struct {
		Kind   string `json:"kind"`
		Bundle string `json:"bundle"`
	}{c.Kind, c.Bundle})
	digest := sha256.Sum256(raw)
	return Request{ID: c.ID, Kind: c.Kind, IntentSHA256: hex.EncodeToString(digest[:])}, nil
}
