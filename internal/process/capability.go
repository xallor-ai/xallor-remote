package process

import "runtime"

// Capability describes the process boundary that the target Runtime can
// actually provide. It is an observation for callers, not a security claim:
// native_user runs with the Runtime user's privileges and sandbox is none.
type Capability struct {
	Name        string `json:"name"`
	Available   bool   `json:"available"`
	Privilege   string `json:"privilege"`
	ProcessTree string `json:"process_tree"`
	Sandbox     string `json:"sandbox"`
	Code        string `json:"code,omitempty"`
}

// Describe reports the selected target-side backend without probing or
// changing host policy. The tree value names the cleanup boundary used by the
// real process package on this target OS.
func Describe() Capability {
	c := Capability{
		Name:      "native_user",
		Available: true,
		Privilege: "same_user",
		Sandbox:   "none",
	}
	switch runtime.GOOS {
	case "windows":
		c.ProcessTree = "job_object"
	case "linux":
		c.ProcessTree = "process_group"
	default:
		c.Available = false
		c.Code = "backend_unavailable"
	}
	return c
}
