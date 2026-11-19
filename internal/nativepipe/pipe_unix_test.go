//go:build !windows

package nativepipe

import (
	"errors"
	"testing"

	"golang.org/x/sys/unix"
)

// Descriptor -1 can never name a valid/recycled resource. Its native read
// failure must preserve EBADF without violating io.Reader's nonnegative count.
func TestReadAvailable_should_returnZero_whenDescriptorIsInvalid(t *testing.T) {
	n, err := ReadAvailable(^uintptr(0), make([]byte, 1))
	if n != 0 || !errors.Is(err, unix.EBADF) {
		t.Fatalf("invalid descriptor: n=%d err=%v; want 0, EBADF", n, err)
	}
}
