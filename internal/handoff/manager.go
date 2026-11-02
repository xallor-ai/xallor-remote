package handoff

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Runtime supplies its existing OS process-tree supervision, avoiding a second
// Windows Job / Unix process-group implementation in the Cursor adapter.
type Hooks struct {
	Prepare func(*exec.Cmd)
	Watch   func(*os.Process) (func(), func())
}
type Manager struct {
	mu     sync.Mutex
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
	dir    string
	items  map[string]Receipt
	active map[string]bool
	err    error
	hooks  Hooks
}

func New(parent context.Context, dir string, hooks Hooks) *Manager {
	ctx, cancel := context.WithCancel(parent)
	m := &Manager{ctx: ctx, cancel: cancel, dir: dir, hooks: hooks, items: map[string]Receipt{}, active: map[string]bool{}}
	m.err = m.load()
	return m
}
func (m *Manager) Close() {
	// Serialize cancellation with Submit's Add before waiting for workers.
	m.mu.Lock()
	m.cancel()
	m.mu.Unlock()
	m.wg.Wait()
}
func (m *Manager) Get(id string) (Receipt, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		return Receipt{}, fmt.Errorf("交接记录不可用，请检查本机数据目录。")
	}
	r, ok := m.items[id]
	if !ok {
		return Receipt{}, fmt.Errorf("没有此请求的交接回执。")
	}
	return r, nil
}
func (m *Manager) Submit(r Request, root string) (Receipt, error) {
	if err := r.validate(); err != nil {
		return Receipt{}, err
	}
	r.Workspace = filepath.Clean(r.Workspace)
	r.Session = strings.ToLower(r.Session)
	r.Attachments = append([]Attachment(nil), r.Attachments...)
	hash := fingerprint(r)
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil || m.ctx.Err() != nil {
		return Receipt{}, fmt.Errorf("交接服务不可用。")
	}
	if old, ok := m.items[r.ID]; ok {
		if old.Fingerprint != hash {
			return Receipt{}, fmt.Errorf("请求 ID 已用于不同内容，拒绝覆盖。")
		}
		return old, nil
	}
	key := r.Session
	if len(m.items) >= MaxReceipts || len(m.active) >= 2 || m.active[key] {
		return Receipt{}, fmt.Errorf("回执或并发交接已达上限；同一会话不能并行交接。")
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	receipt := Receipt{ID: r.ID, Fingerprint: hash, Workspace: r.Workspace, Revision: r.Revision, Session: r.Session, Agent: r.Agent, Mode: r.Mode, State: "received", Created: now, Updated: now}
	if err := m.persist(receipt); err != nil {
		m.err = err
		return Receipt{}, fmt.Errorf("回执无法保存，未启动 Agent。")
	}
	m.items[r.ID] = receipt
	m.active[key] = true
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		defer func() { m.mu.Lock(); delete(m.active, key); m.mu.Unlock() }()
		m.run(r, root)
	}()
	return receipt, nil
}
func (m *Manager) update(id, state, code, version, result string, truncated bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		return m.err
	}
	r := m.items[id]
	if terminal(r.State) {
		return fmt.Errorf("交接已结束。")
	}
	r.State = state
	r.Code = code
	r.Updated = time.Now().UTC().Format(time.RFC3339Nano)
	if version != "" {
		r.CLIVersion = version
	}
	r.Result = result
	r.Truncated = truncated
	if err := m.persist(r); err != nil {
		m.err = err
		return err
	}
	m.items[id] = r
	return nil
}
