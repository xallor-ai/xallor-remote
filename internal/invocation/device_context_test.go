package invocation

import (
	"strings"
	"testing"
)

// Resource changes must invalidate both anonymous and principal-scoped work;
// principal renewal within the same resource does not change this boundary.
func TestDeviceContext_should_bindEveryResourceDimension(t *testing.T) {
	id := strings.Repeat("a", 32)
	other := strings.Repeat("b", 32)
	for _, owned := range []bool{false, true} {
		scope := Scope{Authority: id, DeviceUID: id, GrantID: id}
		if owned {
			scope.GrantID, scope.OwnershipID, scope.PrincipalID, scope.TenantID = "", id, id, id
		}
		current := DeviceContext{Authority: id, DeviceUID: id, GrantID: scope.GrantID, OwnershipID: scope.OwnershipID, Inbound: true}
		if !current.Allows(scope) {
			t.Fatal("valid scope denied")
		}
		for _, change := range []func(*DeviceContext){
			func(d *DeviceContext) { d.Authority = other },
			func(d *DeviceContext) { d.DeviceUID = other },
			func(d *DeviceContext) { d.GrantID = other },
			func(d *DeviceContext) { d.OwnershipID = other },
			func(d *DeviceContext) { d.Inbound = false },
		} {
			replaced := current
			change(&replaced)
			if replaced.Allows(scope) {
				t.Fatal("stale resource scope allowed")
			}
		}
	}
	for _, invalid := range []DeviceContext{{}, {Authority: id, DeviceUID: id, OwnershipID: id, GrantID: id}, {Authority: id, DeviceUID: "invalid"}} {
		if invalid.Valid() {
			t.Fatal("malformed context accepted")
		}
	}
	// A device without a grant can synchronize its disabled state, but this
	// cannot authorize any execution scope.
	empty := DeviceContext{Authority: id, DeviceUID: id}
	if !empty.Valid() || empty.Allows(Scope{}) {
		t.Fatal("empty grant context")
	}
}
