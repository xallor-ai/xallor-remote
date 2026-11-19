//go:build linux

package runtimeprocess

import (
	"fmt"
	"os"
)

func openProcessImage(pid int, _ string) (*os.File, error) {
	// procfs refers to the loaded image object, including a sealed memfd or an
	// unlinked executable; reopening a process-reported pathname would not.
	return os.Open(fmt.Sprintf("/proc/%d/exe", pid))
}
