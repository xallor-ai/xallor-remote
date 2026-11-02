package noderun

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"sync"
	"time"

	"github.com/xallor-ai/xallor-remote/internal/process"
	"github.com/xallor-ai/xallor-remote/internal/protocol"
)

func (d *Daemon) runLocalExec(ctx context.Context, link *wsConn, msg protocol.Message) {
	p, err := protocol.ParseExecPayload(msg.Payload)
	if err != nil {
		_ = link.send(protocol.Nack(msg.ExecID, protocol.AgentError))
		return
	}
	_, _, _, workspace, _, inbound, _ := d.store.Snapshot()
	if !inbound {
		_ = link.send(protocol.Nack(msg.ExecID, protocol.InboundDisabled))
		return
	}
	if hardDenyCommand(p.Command) {
		_ = link.send(protocol.Nack(msg.ExecID, protocol.PolicyDeny))
		return
	}
	if needsApprovalCommand(p.Command) {
		preview := p.Command
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

	cmd := buildCmd(p.Command, cwd)
	stdout, stdoutWrite, err := os.Pipe()
	if err != nil {
		_ = link.send(protocol.Nack(msg.ExecID, protocol.AgentError))
		return
	}
	defer stdout.Close()
	defer stdoutWrite.Close()
	stderr, stderrWrite, err := os.Pipe()
	if err != nil {
		_ = link.send(protocol.Nack(msg.ExecID, protocol.AgentError))
		return
	}
	defer stderr.Close()
	defer stderrWrite.Close()
	cmd.Stdout, cmd.Stderr = stdoutWrite, stderrWrite
	start := time.Now()
	child, err := process.Start(ctx, cmd)
	_ = stdoutWrite.Close()
	_ = stderrWrite.Close()
	if err != nil {
		_ = link.send(protocol.Nack(msg.ExecID, startFailureCode(ctx)))
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
	if errors.Is(err, process.ErrCleanup) {
		_ = link.send(protocol.Err(msg.ExecID, protocol.AgentError))
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
	if ctx.Err() == context.Canceled {
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

// Cancellation can win between the pre-start check and os/exec.Start.
func startFailureCode(ctx context.Context) string {
	if ctx.Err() == context.Canceled {
		return protocol.Cancelled
	}
	if ctx.Err() == context.DeadlineExceeded {
		return protocol.ExecTimeout
	}
	return protocol.AgentError
}

func buildCmd(command, cwd string) *exec.Cmd {
	var cmd *exec.Cmd
	if osName() == "windows" {
		cmd = exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-Command", wrapCommand(command))
	} else {
		cmd = exec.Command("bash", "-lc", wrapCommand(command))
	}
	cmd.Dir = cwd
	cmd.Env = os.Environ()
	return cmd
}
