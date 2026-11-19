//go:build !windows

package cli

import "testing"

func diagnoseNativeActivationLocks(*testing.T, string) {}
