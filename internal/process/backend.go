package process

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
)

const NativeUser = "native_user"

// ErrBackendUnavailable is a pre-start refusal. A requested backend is never
// silently replaced by native_user or another provider.
var ErrBackendUnavailable = errors.New("execution backend unavailable")

// Backend owns launch through the same supervised process lifecycle. Future
// providers must retain ErrCleanup and must not report success before settling
// their resource boundary. Registration is target-side, never caller supplied.
type Backend interface {
	Capability() Capability
	Start(context.Context, *exec.Cmd) (*Child, error)
}

type nativeBackend struct{}

func (nativeBackend) Capability() Capability { return Describe() }
func (nativeBackend) Start(ctx context.Context, cmd *exec.Cmd) (*Child, error) {
	return Start(ctx, cmd)
}

func BackendName(name string) string {
	if name == "" {
		return NativeUser
	}
	return name
}

// Select is fail-closed on every platform. Only a real implementation with an
// available supervision boundary can be selected, including for local config.
func Select(name string) (Backend, error) {
	c := DescribeBackend(name)
	if c.Name != NativeUser || !c.Available {
		return nil, fmt.Errorf("%w: %s", ErrBackendUnavailable, c.Name)
	}
	return nativeBackend{}, nil
}

// Resolve prevents the caller from weakening or replacing the target's local
// selection. Omission uses the target setting; an explicit request must match.
func Resolve(configured, requested string) (Backend, error) {
	configured = BackendName(configured)
	if requested != "" && requested != configured {
		return nil, fmt.Errorf("%w: %s", ErrBackendUnavailable, requested)
	}
	return Select(configured)
}

func DescribeBackend(name string) Capability {
	name = BackendName(name)
	if name == NativeUser {
		return Describe()
	}
	return Capability{Name: name, Available: false, Privilege: "unavailable", Sandbox: "none", Code: "backend_unavailable"}
}

// Backends includes reserved names with explicit unavailable capabilities.
// Availability does not imply a privilege drop, filesystem or network sandbox.
func Backends() []Capability {
	return []Capability{Describe(), DescribeBackend("low_privilege"), DescribeBackend("cgroup"), DescribeBackend("container")}
}
