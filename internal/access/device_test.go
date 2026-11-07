package access

import "testing"

// Synchronizing unchanged credentials and toggling inbound preserve the binding;
// rotation, clearing, and device recreation invalidate the prior authorization.
func TestDevice_should_bindToIdentityAndGrant_whenAccessChanges(t *testing.T) {
	d, err := NewDevice("secret", true)
	if err != nil || d.UID == "" || d.Binding().Matches(d.Binding()) {
		t.Fatalf("ungranted identity: %+v %v", d, err)
	}
	d, err = d.WithAccess("first", true)
	if err != nil || !d.Binding().Matches(d.Binding()) {
		t.Fatal("grant did not establish a binding", err)
	}
	original := d.Binding()
	d, err = d.WithAccess("first", false)
	if err != nil || !original.Matches(d.Binding()) || d.Inbound {
		t.Fatal("idempotent synchronization replaced authorization", err)
	}
	d, err = d.WithAccess("second", true)
	if err != nil || original.Matches(d.Binding()) {
		t.Fatal("rotation retained authorization", err)
	}
	d, err = d.WithAccess("first", true)
	if err != nil || original.Matches(d.Binding()) || original.DeviceUID != d.UID {
		t.Fatal("reused credential resurrected authorization", err)
	}
	d, err = d.WithAccess("", true)
	if err != nil || d.GrantID != "" || d.Binding().Matches(d.Binding()) {
		t.Fatal("cleared grant still authorizes", err)
	}
	replacement, err := NewDevice("secret", true)
	if err != nil || replacement.UID == d.UID {
		t.Fatal("recreated device reused its internal identity", err)
	}
	if (Binding{}).Matches(Binding{}) {
		t.Fatal("empty bindings authorized")
	}
}
