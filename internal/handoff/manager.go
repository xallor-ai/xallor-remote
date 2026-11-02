package handoff

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/xallor-ai/xallor-remote/internal/invocation"
	"github.com/xallor-ai/xallor-remote/internal/job"
)

type Manager struct {
	mu     sync.Mutex
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
	dir    string
	items  map[string]record
	active map[string]bool
	err    error
	jobs   *job.Ledger
}

func New(parent context.Context, dir string, jobs *job.Ledger) *Manager {
	ctx, cancel := context.WithCancel(parent)
	m := &Manager{ctx: ctx, cancel: cancel, dir: dir, jobs: jobs, items: map[string]record{}, active: map[string]bool{}}
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
func (m *Manager) Get(scope invocation.Scope, id string) (Receipt, error) {
	if !scope.Valid() || !idPattern.MatchString(id) {
		return Receipt{}, fmt.Errorf("交接身份或请求 ID 无效。")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		return Receipt{}, fmt.Errorf("交接记录不可用，请检查本机数据目录。")
	}
	r, ok := m.items[requestKey(scope, id)]
	if !ok {
		return Receipt{}, fmt.Errorf("没有此请求的交接回执。")
	}
	return r.Receipt, nil
}
func (m *Manager) Submit(scope invocation.Scope, r Request, root string) (Receipt, error) {
	if !scope.Valid() {
		return Receipt{}, fmt.Errorf("交接身份无效。")
	}
	if err := r.validate(); err != nil {
		return Receipt{}, err
	}
	r.Workspace = filepath.Clean(r.Workspace)
	r.Session = strings.ToLower(r.Session)
	r.Attachments = append([]Attachment(nil), r.Attachments...)
	hash := fingerprint(r)
	id := requestKey(scope, r.ID)
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil || m.ctx.Err() != nil {
		return Receipt{}, fmt.Errorf("交接服务不可用。")
	}
	if old, ok := m.items[id]; ok {
		if old.Fingerprint != hash {
			return Receipt{}, fmt.Errorf("请求 ID 已用于不同内容，拒绝覆盖。")
		}
		return old.Receipt, nil
	}
	key := r.Session
	if !m.ownsSession(scope, key) {
		return Receipt{}, fmt.Errorf("该会话不能用于当前身份，请使用新会话。")
	}
	if len(m.items) >= MaxReceipts || m.active[key] {
		return Receipt{}, fmt.Errorf("回执或并发交接已达上限；同一会话不能并行交接。")
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	owner, err := m.jobs.Reserve(scope, "handoff", r.Session)
	if err != nil {
		return Receipt{}, fmt.Errorf("本机作业容量不足或仍有未确认结束的任务。")
	}
	receipt := Receipt{ID: r.ID, Fingerprint: hash, Workspace: r.Workspace, Revision: r.Revision, Session: r.Session, Agent: r.Agent, Mode: r.Mode, State: "received", Created: now, Updated: now}
	stored := record{Scope: scope, Receipt: receipt}
	if err := m.persist(stored); err != nil {
		_ = owner.Finish()
		m.err = err
		return Receipt{}, fmt.Errorf("回执无法保存，未启动 Agent。")
	}
	m.items[id] = stored
	m.active[key] = true
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		defer func() { m.mu.Lock(); delete(m.active, key); m.mu.Unlock() }()
		result := m.run(id, r, root, owner)
		if owner.Finish() != nil {
			result.state, result.code = "unknown", "job_release_unconfirmed"
		}
		_ = m.update(id, result.state, result.code, result.version, result.text, result.truncated)
	}()
	return receipt, nil
}
func (m *Manager) update(id, state, code, version, result string, truncated bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		return m.err
	}
	r, ok := m.items[id]
	if !ok {
		return fmt.Errorf("没有此请求的交接回执。")
	}
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
