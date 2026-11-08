package protocol

// DeviceLifecycleStep is the stable management contract for device identity
// and access transitions. It describes effects and uncertainty; it is not a
// permission check and cannot be invoked through a client operation.
type DeviceLifecycleStep struct {
	Name         string `json:"name"`
	Actor        string `json:"actor"`
	Precondition string `json:"precondition"`
	Effect       string `json:"effect"`
	Scope        string `json:"scope"`
	OnUnknown    string `json:"on_unknown"`
}

type DeviceLifecycleContract struct {
	Version int                   `json:"version"`
	Steps   []DeviceLifecycleStep `json:"steps"`
}

// DeviceLifecycle returns a fresh, ordered contract shared by protocol, CLI,
// MCP and future management UI. Unknown Relay/SQLite outcomes never imply a
// successful transition or safe replay.
func DeviceLifecycle() DeviceLifecycleContract {
	return DeviceLifecycleContract{
		Version: 1,
		Steps: []DeviceLifecycleStep{
			{Name: "enroll", Actor: "device_runtime", Precondition: "local identity has device_id and secret", Effect: "Relay registers or reauthenticates the device; inbound starts disabled", Scope: "new device UID on first registration", OnUnknown: "keep local identity and retry only after connection state is known"},
			{Name: "grant_issue_or_rotate", Actor: "local device operator", Precondition: "local IPC and settings gate", Effect: "persist one new bearer grant and publish its hash; prior bearer is invalid", Scope: "shared grant binding only", OnUnknown: "local desired state is retained; do not issue another grant automatically"},
			{Name: "claim", Actor: "device connection plus active account", Precondition: "unowned device and valid membership", Effect: "create ownership instance and invalidate shared bearer routes", Scope: "tenant + principal + device UID + ownership instance", OnUnknown: "do not report ownership or reuse old routes"},
			{Name: "transfer", Actor: "current owner on device connection", Precondition: "active recipient membership", Effect: "replace ownership, delete old shares, invalidate old routes", Scope: "new ownership instance", OnUnknown: "old and new ownership are not assumed; require fresh status"},
			{Name: "revoke", Actor: "local device secret", Precondition: "device connection can send revoke", Effect: "delete Relay registration and disconnect routes; local profile remains until reset", Scope: "device UID is retired; next registration gets a new UID", OnUnknown: "do not claim Relay deletion; keep local state for reconciliation"},
			{Name: "reset", Actor: "local device operator", Precondition: "explicit confirmation and serialized settings", Effect: "revoke, wipe identity/profile state, stop Runtime", Scope: "new identity on next start", OnUnknown: "retain reset marker and refuse a fresh identity until cleanup completes"},
		},
	}
}
