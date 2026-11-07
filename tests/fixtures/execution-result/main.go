// execution-result is an owned E2E child with observable output and exit modes.
package main

import (
	"fmt"
	"os"
	"strings"
	"time"
)

func main() {
	fmt.Fprintln(os.Stdout, "result-observed-output")
	fmt.Fprintln(os.Stderr, "result-observed-error")
	if len(os.Args) < 2 {
		os.Exit(2)
	}
	switch os.Args[1] {
	case "success":
		return
	case "failure":
		os.Exit(7)
	case "truncated":
		fmt.Fprint(os.Stdout, strings.Repeat("x", 5*1024*1024))
		os.Exit(7)
	case "wait":
		if len(os.Args) == 3 {
			if os.WriteFile(os.Args[2], []byte("ready"), 0600) != nil {
				os.Exit(3)
			}
		}
		time.Sleep(time.Minute)
	default:
		os.Exit(4)
	}
}
