package release

import (
	"context"
	"errors"
	"github.com/xallor-ai/xallor-remote/internal/buildinfo"
)

type fakeProbe struct {
	r   ProbeRequest
	run func(string, string) (buildinfo.Executable, error)
	err error
}

func (p *fakeProbe) Run(context.Context) (buildinfo.Executable, error) {
	i, e := p.run(p.r.Binary, p.r.Resource.ImageSHA256)
	p.err = e
	return i, e
}
func (p *fakeProbe) Close() ([]ProbeClosure, error) {
	if errors.Is(p.err, ErrCleanupUncertain) {
		return []ProbeClosure{{Resource: p.r.Resource}}, p.err
	}
	return []ProbeClosure{{Resource: p.r.Resource, Closed: true}}, nil
}
func (f *fakeControl) PrepareProbe(r ProbeRequest) (ProbeAttempt, error) {
	return &fakeProbe{r: r, run: f.check}, nil
}
func (c *cleanupProbeControl) PrepareProbe(r ProbeRequest) (ProbeAttempt, error) {
	return &fakeProbe{r: r, run: c.check}, nil
}
