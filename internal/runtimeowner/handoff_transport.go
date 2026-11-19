package runtimeowner

import (
	"bytes"
	"encoding/json"
	"io"

	"github.com/xallor-ai/xallor-remote/internal/release"
)

const MaxHandoffBytes = 128 * 1024

type Handoff struct {
	Command Command              `json:"command"`
	Permit  release.OwnerHandoff `json:"permit"`
}

func (h Handoff) request() (Request, error) {
	r, err := h.Command.Request()
	if err != nil {
		return Request{}, err
	}
	if h.Permit.OperationID != r.ID || h.Permit.IntentSHA256 != r.IntentSHA256 {
		return Request{}, ErrConflict
	}
	return r, nil
}

// DecodeHandoff bounds the private envelope; authentication is a separate,
// mandatory step before this command can be admitted to the journal.
func DecodeHandoff(input io.Reader) (Handoff, error) {
	var h Handoff
	raw, err := io.ReadAll(io.LimitReader(input, MaxHandoffBytes+1))
	if err != nil {
		return h, err
	}
	if len(raw) > MaxHandoffBytes {
		return h, ErrInvalid
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(&h) != nil || d.Decode(new(any)) != io.EOF {
		return Handoff{}, ErrInvalid
	}
	_, err = h.request()
	return h, err
}

func EncodeHandoff(h Handoff) ([]byte, error) {
	if _, err := h.request(); err != nil {
		return nil, err
	}
	raw, err := json.Marshal(h)
	if err == nil && len(raw) > MaxHandoffBytes {
		err = ErrInvalid
	}
	return raw, err
}
