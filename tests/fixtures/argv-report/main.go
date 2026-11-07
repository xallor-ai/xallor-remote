// argv-report exposes the arguments a native child received, without a shell.
package main

import (
	"encoding/json"
	"os"
)

func main() {
	cwd, err := os.Getwd()
	if err != nil {
		os.Exit(43)
	}
	_ = json.NewEncoder(os.Stdout).Encode(struct {
		Args []string `json:"args"`
		Cwd  string   `json:"cwd"`
	}{os.Args[1:], cwd})
	if len(os.Args) > 1 && os.Args[1] == "exit-seven" {
		os.Exit(7)
	}
}
