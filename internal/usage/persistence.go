package usage

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"unicode/utf8"
)

// One atomic envelope keeps retained events and retired-key conflict metadata
// consistent. Old array-only files are rejected, never silently reinterpreted.
type diskState struct {
	Version     int      `json:"version"`
	Events      []Event  `json:"events"`
	RetiredKeys []string `json:"retired_keys"`
}

func keyDigest(key string) string {
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:])
}

func (l *Ledger) load() error {
	f, err := os.Open(l.path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxBytes+1))
	if err != nil {
		return err
	}
	if len(data) > maxBytes || !utf8.Valid(data) {
		return ErrInvalidLedger
	}
	var state diskState
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&state) != nil || decoder.Decode(new(any)) != io.EOF || state.Version != 1 || state.Events == nil || state.RetiredKeys == nil || len(state.Events) > MaxEvents || len(state.RetiredKeys) > MaxRetiredKeys {
		return ErrInvalidLedger
	}
	retired := make(map[string]struct{}, len(state.RetiredKeys))
	for _, key := range state.RetiredKeys {
		decoded, err := hex.DecodeString(key)
		if err != nil || len(decoded) != sha256.Size || hex.EncodeToString(decoded) != key {
			return ErrInvalidLedger
		}
		if _, exists := retired[key]; exists {
			return ErrInvalidLedger
		}
		retired[key] = struct{}{}
	}
	byKey := make(map[string]Event, len(state.Events))
	for _, event := range state.Events {
		if validate(event) != nil {
			return ErrInvalidLedger
		}
		if _, exists := byKey[event.IdempotencyKey]; exists {
			return ErrInvalidLedger
		}
		if _, exists := retired[keyDigest(event.IdempotencyKey)]; exists {
			return ErrInvalidLedger
		}
		byKey[event.IdempotencyKey] = event
	}
	l.events, l.byKey, l.retiredKeys, l.retired = state.Events, byKey, state.RetiredKeys, retired
	return nil
}
