package noderun

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"sync"
	"time"

	"github.com/xallor-ai/xallor-remote/internal/delegation"
	"github.com/xallor-ai/xallor-remote/internal/execution"
	"github.com/xallor-ai/xallor-remote/internal/job"
	"github.com/xallor-ai/xallor-remote/internal/process"
	"github.com/xallor-ai/xallor-remote/internal/protocol"
	"github.com/xallor-ai/xallor-remote/internal/shell"
)

func (d *Daemon) runLocalExec(ctx context.Context, link *wsConn, msg protocol.Message) {
	p, err := execution.Decode(msg.Payload)
	if err != nil {
		_ = link.send(protocol.Nack(msg.ExecID, executionErrorCode(err)))
		return
	}
	_, _, _, _, _, inbound, _ := d.store.Snapshot()
	cfg := d.store.ConfigSnapshot()
	workspace := cfg.Workspace
	if !inbound {
		_ = link.send(protocol.Nack(msg.ExecID, protocol.InboundDisabled))
		return
	}
	backend, err := process.Resolve(cfg.ExecutionBackend, p.Backend)
	if err != nil {
		_ = link.send(protocol.Nack(msg.ExecID, protocol.BackendUnavailable))
		return
	}
	policyText := executionPolicyText(p)
	if hardDenyCommand(policyText) {
		_ = link.send(protocol.Nack(msg.ExecID, protocol.PolicyDeny))
		return
	}
	if needsApprovalCommand(policyText) {
		preview := policyText
		if len(preview) > 200 {
			preview = preview[:200] + "…"
		}
		allow, code := d.approvals.ask(ctx, msg.ExecID, preview)
		if !allow {
			if code == "" {
				code = protocol.PolicyDeny
			}
			_ = link.send(protocol.Nack(msg.ExecID, code))
			return
		}
	}
	cwd := workspace
	if p.Cwd != "" {
		resolved, err := resolveInWorkspace(workspace, p.Cwd)
		if err != nil {
			_ = link.send(protocol.Nack(msg.ExecID, protocol.PolicyDeny))
			return
		}
		cwd = resolved
	}
	if p.TimeoutMS > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(p.TimeoutMS)*time.Millisecond)
		defer cancel()
	}
	if ctx.Err() != nil {
		_ = link.send(protocol.Nack(msg.ExecID, protocol.Cancelled))
		return
	}

	if msg.Scope == nil || !msg.Scope.Valid() {
		_ = link.send(protocol.Nack(msg.ExecID, protocol.Unauthorized))
		return
	}
	guard, stop, err := delegation.Maintain(ctx, msg.ExecID, d.renewForeground)
	if err != nil {
		_ = link.send(protocol.Nack(msg.ExecID, executionLeaseStartCode(ctx, err)))
		return
	}
	defer stop()
	ctx = guard
	cmd, err := execution.Build(p, cfg.Shell, cwd)
	if err != nil {
		_ = link.send(protocol.Nack(msg.ExecID, executionErrorCode(err)))
		return
	}
	owner, err := d.jobs.ReserveExec(*msg.Scope, msg.ExecID)
	if err != nil {
		_ = link.send(protocol.Nack(msg.ExecID, executionAdmissionCode(err)))
		return
	}
	finished := false
	finishOwner := func() error {
		if finished {
			return nil
		}
		finished = true
		return owner.Finish()
	}
	defer func() { _ = finishOwner() }()
	stdout, stdoutWrite, err := os.Pipe()
	if err != nil {
		if releaseErr := finishOwner(); releaseErr != nil {
			_ = link.send(protocol.Err(msg.ExecID, executionResourceCode(releaseErr)))
		} else {
			_ = link.send(protocol.Nack(msg.ExecID, protocol.AgentError))
		}
		return
	}
	defer stdout.Close()
	defer stdoutWrite.Close()
	stderr, stderrWrite, err := os.Pipe()
	if err != nil {
		if releaseErr := finishOwner(); releaseErr != nil {
			_ = link.send(protocol.Err(msg.ExecID, executionResourceCode(releaseErr)))
		} else {
			_ = link.send(protocol.Nack(msg.ExecID, protocol.AgentError))
		}
		return
	}
	defer stderr.Close()
	defer stderrWrite.Close()
	cmd.Stdout, cmd.Stderr = stdoutWrite, stderrWrite
	start := time.Now()
	child, err := owner.StartWithBackend(ctx, cmd, backend.Capability().Name)
	_ = stdoutWrite.Close()
	_ = stderrWrite.Close()
	if err != nil {
		_ = link.send(failedStartMessage(ctx, msg.ExecID, cmd.Process != nil, err, finishOwner()))
		return
	}
	var wg sync.WaitGroup
	var mu sync.Mutex
	sent := 0
	trunc := false
	pump := func(r io.Reader, typ string) {
		defer wg.Done()
		buf := make([]byte, 32*1024)
		for {
			n, err := r.Read(buf)
			if n > 0 {
				mu.Lock()
				chunk, next, t, drain := takeChunk(sent, buf[:n])
				sent = next
				if t {
					trunc = true
				}
				mu.Unlock()
				if len(chunk) > 0 {
					_ = link.send(protocol.Message{Type: typ, ExecID: msg.ExecID, Data: string(chunk)})
				}
				if drain {
					_, _ = io.Copy(io.Discard, r)
					return
				}
			}
			if err != nil {
				return
			}
		}
	}
	wg.Add(2)
	go pump(stdout, protocol.TypeStdout)
	go pump(stderr, protocol.TypeStderr)
	err = child.Wait()
	drained := make(chan struct{})
	go func() { wg.Wait(); close(drained) }()
	select {
	case <-drained:
	case <-time.After(time.Second):
		_ = stdout.Close()
		_ = stderr.Close()
		<-drained
		trunc = true
	}
	releaseErr := finishOwner()
	if releaseErr != nil || errors.Is(err, process.ErrCleanup) {
		_ = link.send(protocol.Err(msg.ExecID, executionResourceCode(errors.Join(releaseErr, err))))
		return
	}
	code := 0
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			code = ee.ExitCode()
		} else {
			code = 1
		}
	}
	status := protocol.ExitCompleted
	if errors.Is(context.Cause(ctx), delegation.ErrDenied) || errors.Is(context.Cause(ctx), delegation.ErrExpired) {
		status = protocol.ExitAuthorizationLost
	} else if ctx.Err() == context.Canceled {
		status = protocol.ExitCancelled
	} else if ctx.Err() == context.DeadlineExceeded {
		status = protocol.ExitTimeout
	}
	dur := time.Since(start).Milliseconds()
	_ = link.send(protocol.Message{
		Type: protocol.TypeExit, ExecID: msg.ExecID, ExitCode: &code,
		DurationMS: &dur, Truncated: protocol.Bool(trunc), Status: status,
	})
}

// A failed start may already have created a process. Cancellation must not
// downgrade that uncertainty (or a failed durable release) to a pre-start nack.
func failedStartMessage(ctx context.Context, id string, started bool, startErr, releaseErr error) protocol.Message {
	if started || errors.Is(startErr, process.ErrCleanup) {
		return protocol.Err(id, protocol.ResourceUncertain)
	}
	if releaseErr != nil {
		return protocol.Err(id, executionResourceCode(errors.Join(startErr, releaseErr)))
	}
	if errors.Is(startErr, process.ErrBackendUnavailable) {
		return protocol.Nack(id, protocol.BackendUnavailable)
	}
	return protocol.Nack(id, startFailureCode(ctx))
}

func executionErrorCode(err error) string {
	switch {
	case errors.Is(err, process.ErrBackendUnavailable):
		return protocol.BackendUnavailable
	case errors.Is(err, execution.ErrInvalid):
		return protocol.InvalidExecution
	case errors.Is(err, shell.ErrUnsupported):
		return protocol.UnsupportedShell
	case errors.Is(err, shell.ErrUnavailable):
		return protocol.ShellUnavailable
	case errors.Is(err, shell.ErrTooLarge), errors.Is(err, execution.ErrTooLarge):
		return protocol.TooLarge
	case errors.Is(err, execution.ErrProgramUnavailable):
		return protocol.ProgramUnavailable
	case errors.Is(err, execution.ErrUnsupportedProgram):
		return protocol.UnsupportedProgram
	default:
		return protocol.AgentError
	}
}

func executionAdmissionCode(err error) string {
	switch {
	case errors.Is(err, job.ErrCapacity):
		return protocol.QuotaExceeded
	case errors.Is(err, job.ErrUncertain):
		return protocol.ResourceUncertain
	case errors.Is(err, job.ErrUnavailable):
		return protocol.ResourceUnavailable
	default:
		return protocol.AgentError
	}
}

func executionResourceCode(err error) string {
	switch {
	case errors.Is(err, job.ErrUncertain), errors.Is(err, process.ErrCleanup):
		return protocol.ResourceUncertain
	case errors.Is(err, job.ErrUnavailable):
		return protocol.ResourceUnavailable
	default:
		return protocol.AgentError
	}
}

// Cancellation can win between the pre-start check and os/exec.Start.
func startFailureCode(ctx context.Context) string {
	if errors.Is(context.Cause(ctx), delegation.ErrDenied) || errors.Is(context.Cause(ctx), delegation.ErrExpired) {
		return protocol.Unauthorized
	}
	if ctx.Err() == context.Canceled {
		return protocol.Cancelled
	}
	if ctx.Err() == context.DeadlineExceeded {
		return protocol.ExecTimeout
	}
	return protocol.AgentError
}
