// Package runtimehead connects local heads to the Runtime owner. It never
// selects an image, bootstraps a process, or retries a business request.
package runtimehead

import (
	"encoding/json"
	"errors"
	"net"
	"time"

	"github.com/xallor-ai/xallor-remote/internal/buildinfo"
	"github.com/xallor-ai/xallor-remote/internal/ipc"
	"github.com/xallor-ai/xallor-remote/internal/protocol"
	"github.com/xallor-ai/xallor-remote/internal/release"
)

// Refusal preserves an owner's structured pre-dispatch failure for machine heads.
type Refusal struct {
	Code    string
	Message string
}

func (e *Refusal) Error() string { return e.Message }

// ErrorCode classifies failures before business dispatch, not execution results.
func ErrorCode(err error) string {
	var refusal *Refusal
	if errors.As(err, &refusal) {
		return refusal.Code
	}
	return protocol.RuntimeUnavailable
}

// Dial opens exactly one connection and confirms that connected owner's
// readiness. Absence does not cause bootstrap here.
func Dial(timeout time.Duration) (*ipc.Conn, error) {
	raw, err := ipc.Dial(timeout)
	if err != nil {
		return nil, err
	}
	return Handshake(raw)
}

// Handshake consumes only the readiness reply and returns the same framed
// connection for business requests. On failure it closes the raw connection;
// on success the caller owns it. Readiness is an observation, not a reservation.
func Handshake(raw net.Conn) (conn *ipc.Conn, err error) {
	defer func() {
		if err != nil {
			_ = raw.Close()
		}
	}()
	if err = raw.SetDeadline(time.Now().Add(65 * time.Second)); err != nil {
		return nil, err
	}
	c := ipc.NewConn(raw)
	const id = "runtime-owner"
	if err = ipc.Call(c, id, "runtime.ensure", map[string]any{
		"required_profile_format": buildinfo.ProfileFormat(), "required_protocol": buildinfo.Protocol,
	}); err != nil {
		return nil, err
	}
	f, err := c.ReadFrame()
	if err != nil {
		return nil, err
	}
	invalid := &Refusal{Code: protocol.RuntimeSourceUnverified, Message: "Runtime 就绪响应缺少有效身份，请检查配套安装。"}
	if f.ID != id || f.Event != "" || f.OK == nil {
		return nil, invalid
	}
	if !*f.OK {
		if f.Code == "" {
			return nil, invalid
		}
		return nil, &Refusal{Code: f.Code, Message: ipc.FailureMessage(f)}
	}
	if f.Code != "" {
		return nil, invalid
	}
	var result struct {
		Runtime release.RuntimeObservation `json:"runtime"`
	}
	if json.Unmarshal(f.Result, &result) != nil || result.Runtime.ValidateReady(buildinfo.ProfileFormat(), buildinfo.Protocol) != nil {
		return nil, invalid
	}
	if err = raw.SetDeadline(time.Time{}); err != nil {
		return nil, err
	}
	return c, nil
}
