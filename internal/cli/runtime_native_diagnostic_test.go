package cli

import (
	"io"
	"os"
	"path/filepath"
	"testing"
)

// Failure-only diagnostics expose the worker's error channel and existence
// markers. Never inspect startup logs, identity files, or handoff credentials.
func logNativeTransactionFailure(t *testing.T, profile string) {
	t.Helper()
	for _, name := range []string{filepath.Join("runtime", "lifecycle", "operations.db"), filepath.Join("runtime", "transaction.log")} {
		_, err := os.Lstat(filepath.Join(profile, name))
		t.Logf("transaction diagnostic: %s exists=%t stat_error=%v", name, err == nil, err)
	}
	path := filepath.Join(profile, "runtime", "transaction.log")
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() {
		return
	}
	file, err := os.Open(path)
	if err != nil {
		t.Logf("transaction diagnostic: error log unavailable: %v", err)
		return
	}
	defer file.Close()
	const limit = 16 * 1024
	if info.Size() > limit {
		if _, err := file.Seek(-limit, io.SeekEnd); err != nil {
			t.Logf("transaction diagnostic: error log seek failed: %v", err)
			return
		}
	}
	data, err := io.ReadAll(io.LimitReader(file, limit))
	t.Logf("transaction diagnostic: transaction.log tail (at most %d bytes, truncated=%t, read_error=%v):\n%s", limit, info.Size() > limit, err, data)
}
