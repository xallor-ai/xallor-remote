package activity

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Retention is bounded and reload returns actual persisted metadata in order.
func TestLog_should_boundAndReloadHistory(t *testing.T) {
	dir := t.TempDir()
	seed := make([]Entry, Limit-1)
	for i := range seed {
		seed[i] = Entry{Stage: "finished"}
	}
	data, _ := json.Marshal(seed)
	if err := os.WriteFile(filepath.Join(dir, "activity.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	l := Open(dir)
	for i := 0; i < 3; i++ {
		if err := l.Append(Entry{ExecID: "test", Stage: "finished", Code: "completed"}); err != nil {
			t.Fatal(err)
		}
	}
	rows, err := Open(dir).Entries()
	if err != nil || len(rows) != Limit {
		t.Fatalf("count=%d err=%v", len(rows), err)
	}
	rows[0].Stage = "changed"
	copy, _ := l.Entries()
	if copy[0].Stage != "finished" {
		t.Fatal("caller changed stored history")
	}
}

// Returning an exit pointer or accepting one must not let callers rewrite history.
func TestLog_should_ownMetadata_and_rejectUnboundedValues(t *testing.T) {
	l := Open(t.TempDir())
	code := 7
	if err := l.Append(Entry{ExitCode: &code}); err != nil {
		t.Fatal(err)
	}
	code = 0
	rows, _ := l.Entries()
	if *rows[0].ExitCode != 7 {
		t.Fatal("retained caller pointer")
	}
	*rows[0].ExitCode = 1
	rows, _ = l.Entries()
	if *rows[0].ExitCode != 7 {
		t.Fatal("exposed history pointer")
	}
	if l.Append(Entry{ExecID: strings.Repeat("x", 97)}) == nil {
		t.Fatal("unbounded metadata")
	}
	if Open("").Append(Entry{}) == nil {
		t.Fatal("empty profile wrote into cwd")
	}
}

// Corrupt history must be reported, never silently presented as an empty successful audit.
func TestLog_should_reportCorruptHistory(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "activity.json"), []byte("broken"), 0600)
	l := Open(dir)
	if _, err := l.Entries(); err == nil {
		t.Fatal("corruption hidden")
	}
	if l.Append(Entry{}) == nil {
		t.Fatal("corrupt history overwritten")
	}
}
