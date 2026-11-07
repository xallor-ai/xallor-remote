//go:build !windows

package execution

func nativeCommandFits(string, []string) bool { return true }
