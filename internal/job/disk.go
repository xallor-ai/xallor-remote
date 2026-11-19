package job

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"runtime"
	"syscall"
)

// Shared bounded decoder for startup and shutdown observation. Opening special
// files must not block before their descriptor type can be rejected.
func readDiskState(path string) (disk diskState, err error) {
	flags := os.O_RDONLY
	if runtime.GOOS != "windows" {
		flags |= syscall.O_NONBLOCK
	}
	f, err := os.OpenFile(path, flags, 0)
	if os.IsNotExist(err) {
		return diskState{Version: 1, Jobs: map[string]Record{}}, nil
	}
	if err != nil {
		return diskState{}, ErrUnavailable
	}
	defer func() {
		if closeErr := f.Close(); closeErr != nil {
			err = ErrUnavailable
		}
	}()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > maxLedgerBytes {
		return diskState{}, ErrUnavailable
	}
	raw, err := io.ReadAll(io.LimitReader(f, maxLedgerBytes+1))
	if err != nil || len(raw) > maxLedgerBytes {
		return diskState{}, ErrUnavailable
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&disk); err != nil || disk.Version != 1 || disk.Jobs == nil || len(disk.Jobs) > Capacity {
		return diskState{}, ErrUnavailable
	}
	if decoder.Decode(new(any)) != io.EOF {
		return diskState{}, ErrUnavailable
	}
	for id, record := range disk.Jobs {
		if !validRecord(id, record) {
			return diskState{}, ErrUnavailable
		}
	}
	return disk, nil
}
