// A test artifact that reports its real identity but fails Runtime startup.
package main

import (
	"encoding/json"
	"github.com/xallor-ai/xallor-remote/internal/buildinfo"
	"os"
)

func main() {
	if len(os.Args) == 3 && os.Args[1] == "version" && os.Args[2] == "--json" {
		_ = json.NewEncoder(os.Stdout).Encode(buildinfo.Running())
		return
	}
	os.Exit(23)
}
