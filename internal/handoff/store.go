package handoff

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/xallor-ai/xallor-remote/internal/atomicfile"
)

type Config struct {
	Executable string   `json:"executable"`
	Prefix     []string `json:"prefix_args,omitempty"`
}

func Configure(dir string, c Config) error {
	if !filepath.IsAbs(dir) {
		return fmt.Errorf("需要绝对数据目录。")
	}
	if err := c.validate(); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(dir, "handoff"), 0700); err != nil {
		return err
	}
	b, _ := json.Marshal(c)
	return atomicfile.Write(filepath.Join(dir, "handoff", "adapter.json"), b)
}

func (c Config) validate() error {
	if !filepath.IsAbs(c.Executable) || len(c.Prefix) > 4 {
		return fmt.Errorf("需要绝对路径，启动前置参数最多 4 个。")
	}
	if info, err := os.Stat(c.Executable); err != nil || !info.Mode().IsRegular() {
		return fmt.Errorf("Cursor CLI 可执行文件不存在。")
	}
	for _, a := range c.Prefix {
		if len(a) > 2048 || strings.ContainsRune(a, 0) {
			return fmt.Errorf("无效的启动参数。")
		}
	}
	return nil
}

func readJSON(path string, limit int64, value any) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil || int64(len(b)) > limit {
		return fmt.Errorf("交接数据读取失败或超过上限。")
	}
	return json.Unmarshal(b, value)
}

func (m *Manager) load() error {
	if !filepath.IsAbs(m.dir) {
		return fmt.Errorf("交接数据目录不可用。")
	}
	entries, err := os.ReadDir(m.receiptDir())
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if len(entries) > MaxReceipts {
		return fmt.Errorf("交接回执超过上限。")
	}
	for _, e := range entries {
		if e.IsDir() || e.Type()&os.ModeSymlink != 0 || !strings.HasSuffix(e.Name(), ".json") {
			return fmt.Errorf("交接回执目录包含未知文件。")
		}
		var r record
		if err := readJSON(filepath.Join(m.receiptDir(), e.Name()), maxReceiptBytes, &r); err != nil {
			return err
		}
		if !r.Scope.Valid() || !idPattern.MatchString(r.ID) || !sessionPattern.MatchString(r.Session) || e.Name() != requestKey(r.Scope, r.ID)+".json" || len(r.Fingerprint) != 64 || !m.ownsSession(r.Scope, r.Session) {
			return fmt.Errorf("交接回执损坏。")
		}
		if !terminal(r.State) {
			r.State = "unknown"
			r.Code = "runtime_restarted"
			r.Updated = time.Now().UTC().Format(time.RFC3339Nano)
			if err := m.persist(r); err != nil {
				return err
			}
		}
		m.items[requestKey(r.Scope, r.ID)] = r
	}
	return nil
}
func (m *Manager) receiptDir() string { return filepath.Join(m.dir, "handoff", "requests") }
func (m *Manager) persist(r record) error {
	if err := os.MkdirAll(m.receiptDir(), 0700); err != nil {
		return err
	}
	b, err := json.Marshal(r)
	if err != nil {
		return err
	}
	if len(b) > maxReceiptBytes {
		return fmt.Errorf("交接回执超过上限。")
	}
	return atomicfile.Write(filepath.Join(m.receiptDir(), requestKey(r.Scope, r.ID)+".json"), b)
}
