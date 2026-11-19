//go:build !windows && !linux

package runtimeprocess

import "os"

func openProcessImage(int, string) (*os.File, error) { return nil, ErrUnsupported }
