package accesscode

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"time"
)

var ErrSessionRequestInvalid = errors.New("access-code session request is invalid")

type SessionRequest struct {
	RequestID        string
	SessionID        string
	TargetUID        string
	CodeID           string
	Generation       uint64
	Scope            []string
	SessionExpiresAt *time.Time
}

type canonicalSessionRequest struct {
	RequestID        string   `json:"request_id"`
	SessionID        string   `json:"session_id"`
	TargetUID        string   `json:"target_uid"`
	CodeID           string   `json:"code_id"`
	Generation       uint64   `json:"generation"`
	Scope            []string `json:"scope"`
	SessionExpiresAt *int64   `json:"session_expires_at,omitempty"`
}

func (r SessionRequest) Digest() (string, error) {
	if r.RequestID == "" || r.SessionID == "" || r.TargetUID == "" || r.CodeID == "" || r.Generation == 0 || len(r.Scope) == 0 {
		return "", ErrSessionRequestInvalid
	}
	seen := make(map[string]struct{}, len(r.Scope))
	scope := make([]string, 0, len(r.Scope))
	for _, item := range r.Scope {
		if item == "" {
			return "", ErrSessionRequestInvalid
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		scope = append(scope, item)
	}
	sort.Strings(scope)
	var expires *int64
	if r.SessionExpiresAt != nil {
		value := r.SessionExpiresAt.UTC().Unix()
		expires = &value
	}
	payload, err := json.Marshal(canonicalSessionRequest{RequestID: r.RequestID, SessionID: r.SessionID, TargetUID: r.TargetUID, CodeID: r.CodeID, Generation: r.Generation, Scope: scope, SessionExpiresAt: expires})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:]), nil
}
