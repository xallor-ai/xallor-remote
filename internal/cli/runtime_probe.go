package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"sync"
	"time"

	"github.com/xallor-ai/xallor-remote/internal/buildinfo"
	"github.com/xallor-ai/xallor-remote/internal/process"
	"github.com/xallor-ai/xallor-remote/internal/release"
	"github.com/xallor-ai/xallor-remote/internal/runtimeimage"
)

const runtimeIdentityProbeTimeout = 30 * time.Second
const runtimeProbeOutputLimit = 64 * 1024

type probeCapture struct {
	data     []byte
	overflow bool
}

func (b *probeCapture) Write(p []byte) (int, error) {
	n := len(p)
	remaining := runtimeProbeOutputLimit - len(b.data)
	if n > remaining {
		b.overflow = true
		p = p[:remaining]
	}
	b.data = append(b.data, p...)
	return n, nil
}

type runtimeProbe struct {
	mu                 sync.Mutex
	request            release.ProbeRequest
	prepare            func(*exec.Cmd, string) (io.Closer, error)
	ran, closed        bool
	info               buildinfo.Executable
	runErr, cleanupErr error
}

func (runtimeControl) PrepareProbe(r release.ProbeRequest) (release.ProbeAttempt, error) {
	return &runtimeProbe{request: r, prepare: func(cmd *exec.Cmd, sha string) (io.Closer, error) {
		image, err := runtimeimage.Prepare(cmd, sha)
		if image == nil {
			return nil, err
		}
		return image, err
	}}, nil
}
func (p *runtimeProbe) Run(parent context.Context) (buildinfo.Executable, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return p.info, errors.New("probe already closed")
	}
	if p.ran {
		return p.info, p.runErr
	}
	p.ran = true
	ctx, cancel := context.WithTimeout(parent, runtimeIdentityProbeTimeout)
	defer cancel()
	cmd := exec.Command(p.request.Binary, "version", "--json")
	process.HideConsole(cmd)
	image, err := p.prepare(cmd, p.request.Resource.ImageSHA256)
	if err != nil {
		p.runErr = runtimeImageAdmissionError(err)
		if errors.Is(p.runErr, release.ErrCleanupUncertain) {
			p.cleanupErr = p.runErr
		}
		if image != nil {
			p.recordClose(image.Close())
		}
		p.runErr = errors.Join(p.runErr, p.cleanupErr)
		return p.info, p.runErr
	}
	if image == nil {
		p.cleanupErr = release.ErrCleanupUncertain
		p.runErr = p.cleanupErr
		return p.info, p.runErr
	}
	var stdout, stderr probeCapture
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	child, startErr := process.Start(ctx, cmd)
	runErr := startErr
	if child != nil {
		runErr = child.Wait()
	}
	if errors.Is(runErr, process.ErrCleanup) || errors.Is(runErr, exec.ErrWaitDelay) {
		p.cleanupErr = errors.Join(release.ErrCleanupUncertain, runErr)
	}
	p.recordClose(image.Close())
	if ctx.Err() != nil {
		runErr = errors.Join(runErr, ctx.Err())
	}
	if stdout.overflow || stderr.overflow {
		runErr = errors.Join(runErr, errors.New("Runtime version output exceeds limit"))
	}
	if runErr == nil {
		runErr = json.Unmarshal(stdout.data, &p.info)
	}
	if runErr == nil && p.info.OwnerProtocol != buildinfo.RuntimeOwnerProtocol {
		runErr = errors.New("candidate does not support the required Runtime owner protocol")
	}
	p.runErr = errors.Join(runErr, p.cleanupErr)
	return p.info, p.runErr
}
func (p *runtimeProbe) recordClose(err error) {
	if err != nil {
		p.cleanupErr = errors.Join(p.cleanupErr, fmt.Errorf("%w: probe image release: %v", release.ErrCleanupUncertain, err))
	}
}
func (p *runtimeProbe) Close() ([]release.ProbeClosure, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.closed = true
	return []release.ProbeClosure{{Resource: p.request.Resource, Closed: p.cleanupErr == nil}}, p.cleanupErr
}

// The recovery bootstrap has no admitted transaction yet. It uses the identical
// native receipt contract under WithBootstrap, and refuses launch on uncertainty;
// its later admitted owner transaction records its own preflight obligations.
func probeRuntimeIdentity(ctx context.Context, path, sha string) (buildinfo.Executable, error) {
	r := release.ProbeRequest{Binary: path, Resource: release.ResourceObligation{ImageSHA256: sha}}
	p, _ := (runtimeControl{}).PrepareProbe(r)
	info, err := p.Run(ctx)
	receipts, closeErr := p.Close()
	if len(receipts) != 1 || receipts[0].Resource != r.Resource || !receipts[0].Closed {
		closeErr = errors.Join(closeErr, release.ErrCleanupUncertain)
	}
	return info, errors.Join(err, closeErr)
}
func probeRuntimeIdentityWithImage(ctx context.Context, path, sha string, prepare func(*exec.Cmd, string) (io.Closer, error)) (buildinfo.Executable, error) {
	p := &runtimeProbe{request: release.ProbeRequest{Binary: path, Resource: release.ResourceObligation{ImageSHA256: sha}}, prepare: prepare}
	info, err := p.Run(ctx)
	_, closeErr := p.Close()
	return info, errors.Join(err, closeErr)
}
