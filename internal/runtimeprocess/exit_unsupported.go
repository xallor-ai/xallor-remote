//go:build !windows && !linux

package runtimeprocess

func watchExit(int) (ExitWatch, error) { return nil, ErrUnsupported }
