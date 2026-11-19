//go:build !linux

package runtimeimage

func sealedPathMatches(_, _ string) bool { return false }
