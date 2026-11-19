package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/xallor-ai/xallor-remote/internal/process"
)

type independentMCPMessage struct {
	ID     string          `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
	Result json.RawMessage `json:"result"`
	Error  json.RawMessage `json:"error"`
}

type independentMCPRead struct {
	message independentMCPMessage
	err     error
}

type independentMCPClient struct {
	t      *testing.T
	input  *os.File
	frames <-chan independentMCPRead
	seq    int
}

type independentMCPOptions struct {
	resumeCapability string
	rotationSecret   string
}

// Ordinary Go runs have no Node/build dependency. Acceptance runs must supply
// both absolute inputs and reject skips in their outer machine-report gate.
func independentMCPPaths(t *testing.T) (string, string) {
	t.Helper()
	node, entry := os.Getenv("XR_MCP_ACCEPTANCE_NODE"), os.Getenv("XR_MCP_ACCEPTANCE_ENTRY")
	if node == "" && entry == "" {
		t.Skip("explicit built MCP and Node inputs required for stdio acceptance")
	}
	for _, name := range []string{node, entry} {
		info, err := os.Stat(name)
		if !filepath.IsAbs(name) || err != nil || !info.Mode().IsRegular() {
			t.Fatal("MCP acceptance input must be an existing absolute regular file")
		}
	}
	return node, entry
}

// The test owns the Node process group; Runtime ownership remains independent.
// Only JSON-RPC traverses stdin/stdout, never a direct tool callback seam.
func (h *independentHarness) startMCP(p *independentProfile, node, entry string, options ...independentMCPOptions) *independentMCPClient {
	h.t.Helper()
	inputRead, inputWrite, err := os.Pipe()
	if err != nil {
		h.t.Fatal(err)
	}
	outputRead, outputWrite, err := os.Pipe()
	if err != nil {
		inputRead.Close()
		inputWrite.Close()
		h.t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	cmd := exec.Command(node, entry)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = inputRead, outputWrite, io.Discard
	process.HideConsole(cmd)
	var env []string
	for _, value := range os.Environ() {
		key, _, _ := strings.Cut(value, "=")
		if !strings.HasPrefix(strings.ToUpper(key), "XALLOR_REMOTE_") {
			env = append(env, value)
		}
	}
	cmd.Env = append(env, "XALLOR_REMOTE_DATA_DIR="+p.dir,
		"XALLOR_REMOTE_RELAY_URL="+h.relayURL,
		"XALLOR_REMOTE_ACCESS_CODE=independent-exec-secret")
	if len(options) > 0 {
		cmd.Env = append(cmd.Env, "XALLOR_REMOTE_RESUME_CAPABILITY="+options[0].resumeCapability,
			"XALLOR_REMOTE_CODE_SECRET="+options[0].rotationSecret)
	}
	child, err := process.Start(ctx, cmd)
	inputRead.Close()
	outputWrite.Close()
	if err != nil {
		cancel()
		inputWrite.Close()
		outputRead.Close()
		if errors.Is(err, process.ErrCleanup) {
			h.cleanupErrors = append(h.cleanupErrors, "MCP startup cleanup unconfirmed")
		}
		h.t.Fatal("MCP process startup failed", err)
	}
	frames := make(chan independentMCPRead, 16)
	readerDone := make(chan struct{})
	go func() {
		defer close(readerDone)
		scanner := bufio.NewScanner(outputRead)
		scanner.Buffer(make([]byte, 4096), 1024*1024)
		for scanner.Scan() {
			var message independentMCPMessage
			err := json.Unmarshal(scanner.Bytes(), &message)
			select {
			case frames <- independentMCPRead{message, err}:
			case <-ctx.Done():
				return
			}
			if err != nil {
				return
			}
		}
		select {
		case frames <- independentMCPRead{err: errors.Join(io.EOF, scanner.Err())}:
		case <-ctx.Done():
		}
	}()
	h.t.Cleanup(func() {
		expired := ctx.Err() != nil
		// Deliberately stop only this test-owned MCP head. This is teardown, not
		// evidence of remote task cancellation or spontaneous graceful shutdown.
		cancel()
		inputWrite.Close()
		waitErr := child.Wait()
		outputRead.Close()
		<-readerDone
		// Wait has completed. Unix Exited excludes signal termination even when
		// the child was reaped; both normal and signaled terminal states count.
		reaped := false
		signaled := false
		if state := cmd.ProcessState; state != nil && state.Pid() == cmd.Process.Pid {
			if status, ok := state.Sys().(syscall.WaitStatus); ok {
				signaled = status.Signaled()
				reaped = status.Exited() || signaled
			}
		}
		if errors.Is(waitErr, process.ErrCleanup) || !reaped {
			h.cleanupErrors = append(h.cleanupErrors, "MCP process cleanup unconfirmed")
			h.t.Error("MCP process cleanup unconfirmed")
		}
		if expired {
			h.t.Error("MCP process exceeded acceptance lifetime")
		}
		rows, _ := h.observations["mcp_processes"].([]map[string]any)
		h.observations["mcp_processes"] = append(rows, map[string]any{
			"pid": cmd.Process.Pid, "reaped": reaped, "supervised_teardown": true,
			"signaled":          signaled,
			"cleanup_confirmed": reaped && !errors.Is(waitErr, process.ErrCleanup),
		})
	})
	c := &independentMCPClient{t: h.t, input: inputWrite, frames: frames}
	id := c.send("initialize", map[string]any{"protocolVersion": "2025-11-25", "capabilities": map[string]any{}, "clientInfo": map[string]string{"name": "independent-consent-acceptance", "version": "1"}})
	var initialized struct {
		ProtocolVersion string `json:"protocolVersion"`
	}
	if json.Unmarshal(c.receive(id), &initialized) != nil || initialized.ProtocolVersion != "2025-11-25" {
		h.t.Fatal("MCP initialization did not negotiate expected version")
	}
	if err := json.NewEncoder(c.input).Encode(map[string]any{"jsonrpc": "2.0", "method": "notifications/initialized"}); err != nil {
		h.t.Fatal(err)
	}
	return c
}

func (c *independentMCPClient) send(method string, params any) string {
	c.t.Helper()
	c.seq++
	id := fmt.Sprintf("mcp-%d", c.seq)
	if err := json.NewEncoder(c.input).Encode(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params}); err != nil {
		c.t.Fatal("MCP request write failed", err)
	}
	return id
}

func (c *independentMCPClient) receive(id string) json.RawMessage {
	c.t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for {
		message := c.next(deadline)
		if message.ID == "" && strings.HasPrefix(message.Method, "notifications/") {
			continue
		}
		if message.ID != id || len(message.Error) != 0 || len(message.Result) == 0 {
			c.t.Fatal("MCP response correlation or protocol error")
		}
		return message.Result
	}
}

func (c *independentMCPClient) next(deadline time.Time) independentMCPMessage {
	c.t.Helper()
	timer := time.NewTimer(time.Until(deadline))
	defer timer.Stop()
	select {
	case frame := <-c.frames:
		if frame.err != nil {
			c.t.Fatal("MCP output framing failed", frame.err)
		}
		return frame.message
	case <-timer.C:
		c.t.Fatal("MCP request timed out; not an authorization refusal")
		return independentMCPMessage{}
	}
}
