package handoff

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	"github.com/xallor-ai/xallor-remote/internal/invocation"
)

// The trusted scope is stored alongside the public receipt, never obtained from
// Request JSON. Keeping it out of Receipt also avoids exposing other identities.
type record struct {
	Scope   invocation.Scope `json:"scope"`
	Receipt `json:"receipt"`
}

func requestKey(scope invocation.Scope, id string) string {
	b, _ := json.Marshal(struct {
		Scope invocation.Scope `json:"scope"`
		ID    string           `json:"id"`
	}{scope, id})
	hash := sha256.Sum256(b)
	return hex.EncodeToString(hash[:])
}

// A completed receipt still binds its Cursor session. Otherwise another caller
// could recover the first caller's conversation by submitting a new request ID.
func (m *Manager) ownsSession(scope invocation.Scope, session string) bool {
	for _, saved := range m.items {
		if saved.Session == session && saved.Scope != scope {
			return false
		}
	}
	return true
}
