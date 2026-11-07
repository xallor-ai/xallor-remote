package access

import (
	"errors"

	"github.com/xallor-ai/xallor-remote/internal/principal"
	"github.com/xallor-ai/xallor-remote/internal/protocol"
)

var ErrDenied = errors.New("device access is not authorized")
var ErrInvalid = errors.New("invalid device access request")
var ErrLimit = errors.New("device share limit reached")

// Permissions are independent of membership and commercial entitlements.
type Permissions uint64

// These bits are persisted. New operations receive new bits; existing bits must
// never be reassigned by reordering the display list.
var operations = []struct {
	name string
	bit  Permissions
}{
	{protocol.OpInfo, 1 << 0}, {protocol.OpRead, 1 << 1},
	{protocol.OpWrite, 1 << 2}, {protocol.OpExec, 1 << 3},
	{protocol.OpProcesses, 1 << 4}, {protocol.OpHandoffSubmit, 1 << 5},
	{protocol.OpHandoffGet, 1 << 6},
}

func AllPermissions() Permissions {
	var result Permissions
	for _, op := range operations {
		result |= op.bit
	}
	return result
}

func ParsePermissions(names []string) (Permissions, error) {
	var result Permissions
	for _, name := range names {
		found := false
		for _, operation := range operations {
			if name == operation.name {
				result |= operation.bit
				found = true
				break
			}
		}
		if !found {
			return 0, ErrInvalid
		}
	}
	if result == 0 {
		return 0, ErrInvalid
	}
	return result, nil
}

func (p Permissions) Allows(operation string) bool {
	for _, op := range operations {
		if op.name == operation {
			return p&op.bit != 0
		}
	}
	return false
}

func (p Permissions) Names() []string {
	names := []string{}
	for _, op := range operations {
		if p.Allows(op.name) {
			names = append(names, op.name)
		}
	}
	return names
}

// Authorization is a server-derived snapshot. Share IDs change on replacement,
// so removing then restoring the same permissions cannot revive an old client.
type Authorization struct {
	Binding     Binding
	ShareID     string
	Permissions Permissions
	ExpiresAt   int64
}

type Request struct {
	Action       string   `json:"action"`
	MembershipID string   `json:"membership_id,omitempty"`
	ShareID      string   `json:"share_id,omitempty"`
	Operations   []string `json:"operations,omitempty"`
	TTLSeconds   int64    `json:"ttl_seconds,omitempty"`
}

type Share struct {
	ID           string   `json:"id"`
	MembershipID string   `json:"membership_id"`
	Operations   []string `json:"operations"`
	ExpiresAt    int64    `json:"expires_at"`
}

type State struct {
	DeviceUID      string  `json:"device_uid"`
	OwnershipID    string  `json:"ownership_id,omitempty"`
	OwnerTenant    string  `json:"owner_tenant,omitempty"`
	OwnerPrincipal string  `json:"owner_principal,omitempty"`
	Shares         []Share `json:"shares"`
}

type Mutation struct {
	Device Device
	State  State
}

func (d Device) OwnedBy(p principal.Identity) bool {
	return d.OwnershipID != "" && p.PrincipalID != "" && d.OwnerPrincipal == p.PrincipalID && d.OwnerTenant == p.TenantID
}

func (r Request) Validate() error {
	switch r.Action {
	case "claim", "status":
		if r.MembershipID != "" || r.ShareID != "" || len(r.Operations) != 0 || r.TTLSeconds != 0 {
			return ErrInvalid
		}
	case "transfer":
		if r.MembershipID == "" || r.ShareID != "" || len(r.Operations) != 0 || r.TTLSeconds != 0 {
			return ErrInvalid
		}
	case "share":
		if r.MembershipID == "" || r.ShareID != "" || r.TTLSeconds < 60 || r.TTLSeconds > 90*24*60*60 {
			return ErrInvalid
		}
		_, err := ParsePermissions(r.Operations)
		return err
	case "unshare":
		if r.ShareID == "" || r.MembershipID != "" || len(r.Operations) != 0 || r.TTLSeconds != 0 {
			return ErrInvalid
		}
	default:
		return ErrInvalid
	}
	return nil
}
