package access

import "testing"

// Operation permissions never imply cancellation of other connections or an
// unknown future operation, and request-specific fields cannot be mixed.
func TestAuthorization_should_rejectAmbiguousRequestsAndUnknownOperations(t *testing.T) {
	p, err := ParsePermissions([]string{"info", "read", "info"})
	if err != nil || p != 3 || !p.Allows("read") || p.Allows("exec") || p.Allows("cancel") || p.Allows("future") {
		t.Fatal(p, err)
	}
	for _, req := range []Request{
		{Action: "share", MembershipID: "member", Operations: []string{"cancel"}, TTLSeconds: 60},
		{Action: "share", MembershipID: "member", Operations: []string{"info"}, TTLSeconds: 59},
		{Action: "transfer", MembershipID: "member", ShareID: "share"},
		{Action: "claim", MembershipID: "self-selected-owner"},
		{Action: "unshare"}, {Action: "unknown"},
	} {
		if req.Validate() == nil {
			t.Fatalf("accepted %+v", req)
		}
	}
	owned := Binding{DeviceUID: "uid", OwnershipID: "owner-version"}
	if !owned.Matches(owned) || owned.Matches(Binding{DeviceUID: "uid", GrantID: "bearer"}) || (Binding{DeviceUID: "uid", GrantID: "grant", OwnershipID: "owner-version"}).Matches(owned) {
		t.Fatal("binding modes collapsed")
	}
}
