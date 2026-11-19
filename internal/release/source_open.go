package release

import (
	"errors"
	"os"
	"runtime"
	"syscall"
)

// Keep rooted path confinement while making POSIX FIFO opens nonblocking.
// Checking the descriptor after opening avoids a path-based stat/open race.
func openSourceFile(root *os.Root, name string) (*os.File, error) {
	flags := os.O_RDONLY
	if runtime.GOOS != "windows" {
		flags |= syscall.O_NONBLOCK
	}
	file, err := root.OpenFile(name, flags, 0)
	if err != nil {
		return nil, err
	}
	info, err := file.Stat()
	if err != nil {
		return nil, sourceCleanupError(err, file.Close())
	}
	if !info.Mode().IsRegular() {
		return nil, sourceCleanupError(errors.New("发行制品必须为普通文件"), file.Close())
	}
	return file, nil
}
