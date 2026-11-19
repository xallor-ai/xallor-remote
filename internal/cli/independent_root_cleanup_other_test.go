//go:build !windows

package cli

func retryableIndependentRootRemove(error) bool { return false }
