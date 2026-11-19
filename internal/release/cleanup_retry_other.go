//go:build !windows

package release

func retryableActivationDelete(error) bool { return false }
