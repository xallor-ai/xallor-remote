//go:build windows

package release

import (
	"errors"

	"golang.org/x/sys/windows"
)

func retryableActivationDelete(err error) bool {
	return errors.Is(err, windows.ERROR_ACCESS_DENIED) || errors.Is(err, windows.ERROR_SHARING_VIOLATION)
}
