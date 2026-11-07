package buildinfo

import (
	_ "embed"
	"strconv"
	"strings"
)

// ProfileFormat identifies the local persistent data contract, independently of
// the wire protocol. Change it when an older binary cannot safely use the data.
//
//go:embed profile_format.txt
var sourceProfileFormat string

func ProfileFormat() int {
	n, err := strconv.Atoi(strings.TrimSpace(sourceProfileFormat))
	if err != nil || n < 1 {
		panic("invalid embedded profile format")
	}
	return n
}
