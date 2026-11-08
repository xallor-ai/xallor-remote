package process

import (
	"runtime"
	"testing"
)

// The advertised backend must match the target Runtime's real supervision
// boundary and must not imply privilege reduction or OS sandboxing.
func TestDescribe_should_reportNativeUserBoundary_forTargetOS(t *testing.T) {
	c := Describe()
	want := map[string]string{"windows": "job_object", "linux": "process_group"}[runtime.GOOS]
	if c.Name != "native_user" || c.Privilege != "same_user" || c.Sandbox != "none" {
		t.Fatalf("unexpected backend capability: %+v", c)
	}
	if want != "" && !c.Available {
		t.Fatalf("supported target was not advertised as available: %+v", c)
	}
	if want != "" && c.ProcessTree != want {
		t.Fatalf("process tree=%q want %q", c.ProcessTree, want)
	}
	if want == "" && (c.ProcessTree != "" || c.Code != "backend_unavailable" || c.Available) {
		t.Fatalf("unsupported target was advertised as available: %+v", c)
	}
}
