//go:build windows

package execution

import (
	"syscall"
	"unicode/utf16"
)

func nativeCommandFits(executable string, args []string) bool {
	units := len(utf16.Encode([]rune(syscall.EscapeArg(executable)))) + 1
	for _, arg := range args {
		units += 1 + len(utf16.Encode([]rune(syscall.EscapeArg(arg))))
	}
	return units <= 32767
}
