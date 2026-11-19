package runtimeimage

import (
	"path/filepath"
	"runtime"
	"strings"
)

// MatchesPath relates the observed image location to an activation. Callers
// must independently match the running digest, build identity and owned PID.
// A matching pathname or memfd label alone never authorizes an executable.
func MatchesPath(observed, selected string) bool {
	a, errA := filepath.Abs(observed)
	b, errB := filepath.Abs(selected)
	if errA != nil || errB != nil {
		return false
	}
	if runtime.GOOS == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b || sealedPathMatches(observed, selected)
}
