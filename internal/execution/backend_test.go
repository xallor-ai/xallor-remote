package execution

import (
	"errors"
	"strings"
	"testing"
)

// Client-side validation is platform-neutral: preserve named selections for
// target evaluation, but reject malformed/bounded input before transmission.
func TestRequest_shouldPreserveBackendAndCountItsBytes(t *testing.T) {
	for _, name := range []string{"", "native_user", "low_privilege", "cgroup", "container", "unknown"} {
		r, err := Decode([]byte(`{"backend":"` + name + `","program":{"executable":"fixture"}}`))
		if err != nil || r.Backend != name {
			t.Fatalf("backend discarded: %+v %v", r, err)
		}
	}
	for _, name := range []string{strings.Repeat("a", 65), strings.Repeat("中", 22), "\x00"} {
		r := Request{Script: &Script{Text: "echo hi"}, Backend: name}
		if !errors.Is(r.Validate(), ErrInvalid) {
			t.Fatalf("invalid backend accepted: %q", name)
		}
	}
	r := Request{Script: &Script{Text: strings.Repeat("a", MaxInputBytes)}, Backend: "native_user"}
	if !errors.Is(r.Validate(), ErrTooLarge) {
		t.Fatal("backend omitted from input budget")
	}
}
