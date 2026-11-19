//go:build !windows

package release

import (
	"os"

	"golang.org/x/sys/unix"
)

func openSharedState(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_RDONLY|unix.O_NONBLOCK|unix.O_NOFOLLOW, 0)
}
