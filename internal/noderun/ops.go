package noderun

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/xallor-ai/xallor-remote/internal/buildinfo"
	"github.com/xallor-ai/xallor-remote/internal/identity"
	"github.com/xallor-ai/xallor-remote/internal/process"
	"github.com/xallor-ai/xallor-remote/internal/protocol"
	"github.com/xallor-ai/xallor-remote/internal/shell"
)

func (d *Daemon) runLocalRead(link *wsConn, msg protocol.Message) {
	start := time.Now()
	var p protocol.ReadPayload
	if err := json.Unmarshal(msg.Payload, &p); err != nil || p.Path == "" {
		_ = link.send(protocol.Nack(msg.ExecID, protocol.AgentError))
		return
	}
	_, _, _, workspace, _, inbound, _ := d.store.Snapshot()
	if !inbound {
		_ = link.send(protocol.Nack(msg.ExecID, protocol.InboundDisabled))
		return
	}
	st, err := statWorkspaceFile(workspace, p.Path)
	if err != nil {
		nackReadErr(link, msg.ExecID, err)
		return
	}
	if st.IsDir() {
		_ = link.send(protocol.Nack(msg.ExecID, protocol.PolicyDeny))
		return
	}
	if readTooLarge(st.Size(), p) {
		_ = link.send(protocol.Nack(msg.ExecID, protocol.TooLarge))
		return
	}
	raw, preTruncated, err := readWorkspaceWindow(workspace, p, st.Size())
	if err != nil {
		nackReadErr(link, msg.ExecID, err)
		return
	}
	limit := p.Limit
	if limit <= 0 {
		limit = protocol.DefaultReadBytes
	}
	if limit > protocol.MaxReadBytes {
		limit = protocol.MaxReadBytes
	}
	if bytes.IndexByte(raw, 0) >= 0 {
		_ = link.send(protocol.Nack(msg.ExecID, protocol.PolicyDeny))
		return
	}
	// readWorkspaceWindow already applied the selector while streaming. Apply
	// only the final byte/rune budget here; applying offset a second time would
	// treat the selected window as the original file and lose truncation state.
	selector := protocol.ReadPayload{Head: p.Head, Tail: p.Tail}
	out, trunc := sliceRead(raw, selector, limit)
	trunc = trunc || preTruncated
	if err := sendStdoutFrames(link, msg.ExecID, out); err != nil {
		if err == errMsgTooLarge {
			_ = link.send(protocol.Nack(msg.ExecID, protocol.TooLarge))
			return
		}
		_ = link.send(protocol.Nack(msg.ExecID, protocol.AgentError))
		return
	}
	d.sendOKExit(link, msg.ExecID, start, trunc)
}

func splitLines(raw []byte) [][]byte {
	if len(raw) == 0 {
		return nil
	}
	lines := bytes.Split(raw, []byte("\n"))
	if len(lines) > 0 && len(lines[len(lines)-1]) == 0 {
		lines = lines[:len(lines)-1]
	}
	return lines
}

func sliceRead(raw []byte, p protocol.ReadPayload, limit int) ([]byte, bool) {
	if p.Head > 0 {
		lines := splitLines(raw)
		if p.Head < len(lines) {
			raw = bytes.Join(lines[:p.Head], []byte("\n"))
		}
	} else if p.Tail > 0 {
		lines := splitLines(raw)
		if p.Tail < len(lines) {
			raw = bytes.Join(lines[len(lines)-p.Tail:], []byte("\n"))
		}
	} else if p.Offset > 0 || p.Limit > 0 {
		off := snapUTF8Start(raw, p.Offset)
		if off >= len(raw) {
			return nil, false
		}
		end := len(raw)
		if p.Limit > 0 && off+p.Limit < end {
			end = off + p.Limit
		}
		raw = raw[off:end]
	}
	cut := false
	if len(raw) > limit {
		raw = raw[:limit]
		cut = true
	}
	out := clampUTF8Prefix(raw)
	if cut || len(out) < len(raw) {
		return out, true
	}
	return out, false
}

func clampUTF8Prefix(b []byte) []byte {
	for len(b) > 0 && !utf8.Valid(b) {
		b = b[:len(b)-1]
	}
	return b
}

// snapUTF8Start moves a byte offset back to the start of the rune that contains it.
func snapUTF8Start(raw []byte, off int) int {
	if off <= 0 {
		return 0
	}
	if off >= len(raw) {
		return len(raw)
	}
	for off > 0 && !utf8.RuneStart(raw[off]) {
		off--
	}
	return off
}

func (d *Daemon) runLocalWrite(link *wsConn, msg protocol.Message) {
	start := time.Now()
	var p protocol.WritePayload
	if err := json.Unmarshal(msg.Payload, &p); err != nil || p.Path == "" {
		_ = link.send(protocol.Nack(msg.ExecID, protocol.AgentError))
		return
	}
	if len(p.Content) > protocol.MaxWriteBytes {
		_ = link.send(protocol.Nack(msg.ExecID, protocol.TooLarge))
		return
	}
	_, _, _, workspace, _, inbound, _ := d.store.Snapshot()
	if !inbound {
		_ = link.send(protocol.Nack(msg.ExecID, protocol.InboundDisabled))
		return
	}
	if err := writeWorkspaceFile(workspace, p.Path, p.Content); err != nil {
		code := protocol.AgentError
		if pathDenied(err) {
			code = protocol.PolicyDeny
		}
		_ = link.send(protocol.Nack(msg.ExecID, code))
		return
	}
	d.sendOKExit(link, msg.ExecID, start, false)
}

func readTooLarge(size int64, p protocol.ReadPayload) bool {
	if p.Offset < 0 || p.Limit < 0 || p.Head < 0 || p.Tail < 0 || p.Offset > protocol.MaxReadOffsetBytes {
		return true
	}
	if p.Head > 0 || p.Tail > 0 {
		count := p.Head
		if count == 0 {
			count = p.Tail
		}
		if count > protocol.MaxReadLines {
			return true
		}
	}
	if p.Head != 0 || p.Tail != 0 || p.Limit != 0 || p.Offset != 0 {
		return false
	}
	return size > int64(protocol.MaxReadBytes)
}

// readWorkspaceWindow never materializes the whole target for a selected
// window. Offset reads seek directly; head/tail keep only the requested byte
// budget. The old readWorkspaceFile path remains for internal non-remote uses.
func readWorkspaceWindow(workspace string, p protocol.ReadPayload, size int64) ([]byte, bool, error) {
	rel, err := workspaceRel(workspace, p.Path)
	if err != nil {
		return nil, false, err
	}
	root, err := openWorkspace(workspace)
	if err != nil {
		return nil, false, err
	}
	defer root.Close()
	f, err := root.Open(rel)
	if err != nil {
		return nil, false, err
	}
	defer f.Close()
	limit := p.Limit
	if limit <= 0 || limit > protocol.MaxReadBytes {
		limit = protocol.MaxReadBytes
	}
	if p.Head == 0 && p.Tail == 0 && (p.Offset != 0 || p.Limit != 0) {
		start := p.Offset - 4
		if start < 0 {
			start = 0
		}
		if _, err := f.Seek(int64(start), io.SeekStart); err != nil {
			return nil, false, err
		}
		data, err := io.ReadAll(io.LimitReader(f, int64(limit+p.Offset-start+1)))
		if err != nil {
			return nil, false, err
		}
		out, truncated := sliceRead(data, protocol.ReadPayload{Offset: p.Offset - start, Limit: p.Limit}, limit)
		return out, truncated, nil
	}
	if p.Head > 0 {
		lineBudget := p.Limit
		if lineBudget == 0 {
			lineBudget = protocol.DefaultReadBytes
		} else if lineBudget > protocol.MaxReadBytes {
			lineBudget = protocol.MaxReadBytes
		}
		if size <= int64(protocol.MaxReadBytes) {
			lineBudget = protocol.MaxReadBytes
		}
		reader := bufio.NewReader(f)
		var out []byte
		truncated := false
		for i := 0; i < p.Head; i++ {
			line, clipped, readErr := readLinePrefix(reader, lineBudget, protocol.MaxReadBytes-len(out))
			if readErr != nil && readErr != io.EOF {
				return nil, false, readErr
			}
			truncated = truncated || clipped
			out = append(out, line...)
			if len(out) >= protocol.MaxReadBytes || readErr == io.EOF {
				truncated = truncated || readErr != io.EOF || len(out) >= protocol.MaxReadBytes
				break
			}
		}
		return out, truncated, nil
	}
	if p.Tail > 0 {
		start, err := tailStart(f, size, p.Tail)
		if err != nil {
			return nil, false, err
		}
		if _, err := f.Seek(start, io.SeekStart); err != nil {
			return nil, false, err
		}
		data, err := io.ReadAll(io.LimitReader(f, int64(protocol.MaxReadBytes)+1))
		return data, len(data) > protocol.MaxReadBytes, err
	}
	if size > int64(protocol.MaxReadBytes) {
		return nil, false, os.ErrInvalid
	}
	data, err := io.ReadAll(io.LimitReader(f, int64(protocol.MaxReadBytes)+1))
	return data, len(data) > protocol.MaxReadBytes, err
}

// readLinePrefix consumes a complete line in fixed-size chunks while retaining
// only the requested prefix and reporting whether bytes were discarded.
func readLinePrefix(reader *bufio.Reader, lineBudget, aggregateBudget int) ([]byte, bool, error) {
	keep := lineBudget
	if aggregateBudget < keep {
		keep = aggregateBudget
	}
	line := make([]byte, 0, minInt(keep, 4096))
	clipped := false
	delimiter := false
	for {
		chunk, err := reader.ReadSlice('\n')
		if bytes.IndexByte(chunk, '\n') >= 0 {
			delimiter = true
		}
		before := len(line)
		if before < keep {
			n := keep - before
			if n > len(chunk) {
				n = len(chunk)
			}
			line = append(line, chunk[:n]...)
			if len(chunk) > n {
				clipped = true
			}
		} else if len(chunk) > 0 {
			clipped = true
		}
		if err != bufio.ErrBufferFull {
			if err == io.EOF && len(chunk) == 0 && len(line) == 0 {
				return nil, clipped, io.EOF
			}
			if len(chunk) > len(line) && keep <= len(line) {
				clipped = true
			}
			if clipped && delimiter && keep > 0 {
				if keep == 1 {
					line = []byte{'\n'}
				} else {
					if len(line) >= keep {
						line = line[:keep-1]
					}
					line = clampUTF8Prefix(line)
					line = append(line, '\n')
				}
			}
			return line, clipped, err
		}
		if len(line) >= keep {
			clipped = true
		}
	}
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func tailStart(f *os.File, size int64, lines int) (int64, error) {
	if size <= 0 {
		return 0, nil
	}
	buf := make([]byte, 64*1024)
	pos, remaining := size, lines
	for pos > 0 {
		n := int64(len(buf))
		if n > pos {
			n = pos
		}
		pos -= n
		if _, err := f.ReadAt(buf[:n], pos); err != nil && err != io.EOF {
			return 0, err
		}
		for i := int(n) - 1; i >= 0; i-- {
			absolute := pos + int64(i)
			if absolute == size-1 {
				continue
			} // trailing newline is not an empty line
			if buf[i] == '\n' {
				remaining--
				if remaining == 0 {
					return absolute + 1, nil
				}
			}
		}
	}
	return 0, nil
}

func pathDenied(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	return strings.Contains(s, "outside workspace") ||
		strings.Contains(s, "path too long") ||
		strings.Contains(s, "sensitive path") ||
		strings.Contains(s, "path escapes from parent")
}

func nackReadErr(link *wsConn, execID string, err error) {
	if os.IsNotExist(err) {
		_ = link.send(protocol.Nack(execID, protocol.AgentError))
		return
	}
	_ = link.send(protocol.Nack(execID, protocol.PolicyDeny))
}

func stdoutFrameBudget() int {
	max := protocol.MaxFrameBytes
	wire := protocol.MaxWSMessageBytes - protocol.MaxEnvelopeBytes
	if wire > 0 && max > wire/6 {
		max = wire / 6
	}
	if max < 1 {
		return 1
	}
	return max
}

func nextUTF8Frame(data []byte, max int) (frame, rest []byte) {
	i := 0
	for i < len(data) && !utf8.RuneStart(data[i]) {
		i++
	}
	data = data[i:]
	if len(data) == 0 {
		return nil, nil
	}
	if max > len(data) {
		max = len(data)
	}
	for max > 0 && !utf8.Valid(data[:max]) {
		max--
	}
	if max == 0 {
		return nil, nil
	}
	return data[:max], data[max:]
}

func sendStdoutFrames(link *wsConn, execID string, data []byte) error {
	max := stdoutFrameBudget()
	for len(data) > 0 {
		frame, rest := nextUTF8Frame(data, max)
		if len(frame) == 0 {
			return nil
		}
		if err := link.send(protocol.Message{Type: protocol.TypeStdout, ExecID: execID, Data: string(frame)}); err != nil {
			if err == errMsgTooLarge && max > 1 {
				max /= 2
				continue
			}
			return err
		}
		data = rest
	}
	return nil
}

func (d *Daemon) runLocalProcesses(link *wsConn, msg protocol.Message) {
	start := time.Now()
	_, _, _, _, _, inbound, _ := d.store.Snapshot()
	if !inbound {
		_ = link.send(protocol.Nack(msg.ExecID, protocol.InboundDisabled))
		return
	}
	out, err := listProcesses()
	if err != nil {
		_ = link.send(protocol.Nack(msg.ExecID, protocol.AgentError))
		return
	}
	if len(out) > protocol.MaxFrameBytes {
		out = out[:protocol.MaxFrameBytes]
	}
	_ = link.send(protocol.Message{Type: protocol.TypeStdout, ExecID: msg.ExecID, Data: string(out)})
	d.sendOKExit(link, msg.ExecID, start, false)
}

func (d *Daemon) runLocalInfo(link *wsConn, msg protocol.Message) {
	start := time.Now()
	id, _, _, workspace, _, inbound, _ := d.store.Snapshot()
	if !inbound {
		_ = link.send(protocol.Nack(msg.ExecID, protocol.InboundDisabled))
		return
	}
	goos, arch := identity.OSArch()
	body, _ := json.Marshal(map[string]any{
		"device_id":             id,
		"os":                    goos,
		"arch":                  arch,
		"hostname":              identity.Hostname(),
		"version":               buildinfo.Version(),
		"build":                 buildinfo.Current(),
		"workspace":             workspace,
		"shell":                 shell.Describe(d.store.ConfigSnapshot().Shell),
		"execution_modes":       []string{"script", "program"},
		"execution_backend":     process.Describe(),
		"operation_policy":      protocol.PolicyMatrix(),
		"device_lifecycle":      protocol.DeviceLifecycle(),
		"entitlement_contract":  protocol.EntitlementContract(),
		"usage_event_contract":  protocol.UsageEventContract(),
		"relay_registry":        protocol.RelayRegistry(),
		"relay_failover":        protocol.RelayFailover(),
		"plane_contract":        protocol.ControlDataPlane(),
		"oidc_mapping_contract": protocol.OIDCMapping(),
		"docker":                false,
		"nvidia":                false,
	})
	_ = link.send(protocol.Message{Type: protocol.TypeStdout, ExecID: msg.ExecID, Data: string(body)})
	d.sendOKExit(link, msg.ExecID, start, false)
}

func (d *Daemon) sendOKExit(link *wsConn, execID string, start time.Time, truncated bool) {
	code := 0
	dur := time.Since(start).Milliseconds()
	_ = link.send(protocol.Message{
		Type: protocol.TypeExit, ExecID: execID, ExitCode: &code,
		DurationMS: &dur, Truncated: protocol.Bool(truncated), Status: protocol.ExitCompleted,
	})
}

func listProcesses() ([]byte, error) {
	cmd := exec.Command("tasklist")
	if osName() != "windows" {
		cmd = exec.Command("ps", "-u", os.Getenv("USER"), "-o", "pid,comm")
	}
	return cmd.Output()
}
