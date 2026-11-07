package invocation

// DeviceContext is the Relay's current resource incarnation, delivered only to
// its authenticated device connection. It is not a principal access decision.
type DeviceContext struct {
	Authority   string `json:"authority"`
	DeviceUID   string `json:"device_uid"`
	OwnershipID string `json:"ownership_id,omitempty"`
	GrantID     string `json:"grant_id,omitempty"`
	Inbound     bool   `json:"inbound"`
}

func (d DeviceContext) Valid() bool {
	return identity(d.Authority) && identity(d.DeviceUID) &&
		(d.OwnershipID == "" || identity(d.OwnershipID)) &&
		(d.GrantID == "" || identity(d.GrantID)) &&
		(d.OwnershipID == "" || d.GrantID == "")
}

func (d DeviceContext) Allows(scope Scope) bool {
	return d.Valid() && d.Inbound && scope.Valid() &&
		d.Authority == scope.Authority && d.DeviceUID == scope.DeviceUID &&
		d.OwnershipID == scope.OwnershipID && d.GrantID == scope.GrantID
}

func (d DeviceContext) RetainedBytes() int {
	return 128 + len(d.Authority) + len(d.DeviceUID) + len(d.OwnershipID) + len(d.GrantID)
}
