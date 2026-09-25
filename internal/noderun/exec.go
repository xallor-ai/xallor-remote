package noderun

import (
	"context"
	"io"
	"os"
	"os/exec"
	"sync"
	"time"

	"github.com/xallor-ai/xallor-remote/internal/protocol"
)

func (d *Daemon) runLocalExec(link *wsConn, msg protocol.Message) {
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
		allow, code := d.approvals.ask(msg.ExecID, preview)
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
	ctx := d.ctx
	if p.TimeoutMS > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(p.TimeoutMS)*time.Millisecond)
		defer cancel()
	}
	ctx, cancel := context.WithCancel(ctx)
	d.mu.Lock()
	d.local[msg.ExecID] = cancel
	d.mu.Unlock()
	defer func() {
		cancel()
		d.mu.Lock()
		delete(d.local, msg.ExecID)
		d.mu.Unlock()
	}()

	cmd := buildCmd(ctx, p.Command, cwd)
	stdout, _ := cmd.StdoutPipe()
	stderr, _ := cmd.StderrPipe()
	start := time.Now()
	if err := cmd.Start(); err != nil {
		_ = link.send(protocol.Nack(msg.ExecID, protocol.AgentError))
		return
	}
	kill, closeJob := startJob(cmd.Process)
	defer closeJob()
	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			select {
			case <-done:
			default:
				kill()
			}
		case <-done:
		}
	}()
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
	err = cmd.Wait()
	close(done)
	wg.Wait()
	code := 0
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
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

func buildCmd(ctx context.Context, command, cwd string) *exec.Cmd {
	var cmd *exec.Cmd
	if osName() == "windows" {
		cmd = exec.CommandContext(ctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-Command", wrapCommand(command))
	} else {
		cmd = exec.CommandContext(ctx, "bash", "-lc", wrapCommand(command))
	}
	cmd.Dir = cwd
	cmd.Env = os.Environ()
	cmd.SysProcAttr = procAttr()
	return cmd
}
