package release

import (
	"context"
	"github.com/xallor-ai/xallor-remote/internal/buildinfo"
)

// PrepareProbe allocates no native resources. Run may allocate only after the
// coordinator has persisted Resource. Close is idempotent, retaining its first
// cleanup result; a process exit error alone does not establish closure.
type ProbeRequest struct {
	Binary         string
	ManifestSHA256 string
	Resource       ResourceObligation
}
type ProbeClosure struct {
	Resource ResourceObligation
	Closed   bool
}
type ProbeAttempt interface {
	Run(context.Context) (buildinfo.Executable, error)
	Close() ([]ProbeClosure, error)
}
