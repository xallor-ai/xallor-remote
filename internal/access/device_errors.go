package access

import "errors"

var (
	ErrDeviceNotFound = errors.New("device not found")
	ErrDeviceIdentity = errors.New("device identity does not match")
)
