// Package activity persists bounded operation metadata, never command bodies or credentials.
package activity

import (
	"encoding/json"
	"fmt"
	"github.com/xallor-ai/xallor-remote/internal/atomicfile"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"
	"unicode/utf8"
)

const Limit = 500

type Entry struct {
	Time      string `json:"time"`
	ExecID    string `json:"exec_id"`
	Direction string `json:"direction"`
	DeviceID  string `json:"device_id"`
	Op        string `json:"op,omitempty"`
	Stage     string `json:"stage"`
	Code      string `json:"code,omitempty"`
	ExitCode  *int   `json:"exit_code,omitempty"`
}

type Log struct {
	mu      sync.Mutex
	path    string
	entries []Entry
	err     error
}

func Open(dir string) *Log {
	l := &Log{path: filepath.Join(dir, "activity.json"), entries: []Entry{}}
	if !filepath.IsAbs(dir) {
		l.err = fmt.Errorf("审计目录必须为绝对路径。")
		return l
	}
	f, err := os.Open(l.path)
	if os.IsNotExist(err) {
		return l
	}
	if err != nil {
		l.err = err
		return l
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, 512*1024+1))
	if err != nil || len(data) > 512*1024 {
		l.err = fmt.Errorf("审计文件无法读取或超过上限。")
		return l
	}
	if err = json.Unmarshal(data, &l.entries); err != nil || len(l.entries) > Limit {
		l.err = fmt.Errorf("审计文件损坏。")
		l.entries = nil
	}
	return l
}

func (l *Log) Append(e Entry) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.err != nil {
		return l.err
	}
	// All fields are metadata. Bound them so every file we write can be reloaded.
	for _, value := range []string{e.ExecID, e.Direction, e.DeviceID, e.Op, e.Stage, e.Code} {
		if len(value) > 96 || !utf8.ValidString(value) {
			return fmt.Errorf("invalid audit metadata")
		}
	}
	e = clone(e)
	e.Time = time.Now().UTC().Format(time.RFC3339Nano)
	next := append(append([]Entry(nil), l.entries...), e)
	if len(next) > Limit {
		next = next[len(next)-Limit:]
	}
	data, err := json.Marshal(next)
	if err != nil {
		return err
	}
	// JSON escaping can expand metadata. Reject rather than persist an unreadable file.
	if len(data) > 512*1024 {
		return fmt.Errorf("audit metadata exceeds limit")
	}
	err = atomicfile.Write(l.path, data)
	if err != nil {
		l.err = err
		return err
	}
	l.entries = next
	return nil
}

func (l *Log) Entries() ([]Entry, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.err != nil {
		return nil, fmt.Errorf("审计记录不可用，请检查本机数据目录。")
	}
	rows := make([]Entry, len(l.entries))
	for i, e := range l.entries {
		rows[i] = clone(e)
	}
	return rows, nil
}

func clone(e Entry) Entry {
	if e.ExitCode != nil {
		code := *e.ExitCode
		e.ExitCode = &code
	}
	return e
}
