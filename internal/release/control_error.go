package release

import "errors"

// ErrCleanupUncertain means process termination or image release has not been
// confirmed. It prohibits automatically starting a replacement or restoration.
var ErrCleanupUncertain = errors.New("runtime cleanup is uncertain")

// ErrUnmanagedBaseline refuses a tracked transition without a selected launch.
var ErrUnmanagedBaseline = errors.New("running Runtime has no managed launch identity; installation refused")
