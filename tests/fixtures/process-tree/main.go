// A harmless parent/child process fixture; only writes inside the supplied test directory.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

func main() {
	if len(os.Args) != 3 {
		os.Exit(2)
	}
	dir := os.Args[2]
	if os.Args[1] == "child" {
		for {
			_ = os.WriteFile(filepath.Join(dir, "heartbeat"), []byte(time.Now().UTC().Format(time.RFC3339Nano)), 0600)
			time.Sleep(40 * time.Millisecond)
		}
	}
	if os.Args[1] != "parent" {
		os.Exit(2)
	}
	self, err := os.Executable()
	if err != nil {
		panic(err)
	}
	child := exec.Command(self, "child", dir)
	if err = child.Start(); err != nil {
		panic(err)
	}
	data, _ := json.Marshal(map[string]int{"parent": os.Getpid(), "child": child.Process.Pid})
	if err = os.WriteFile(filepath.Join(dir, "pids.json"), data, 0600); err != nil {
		panic(err)
	}
	fmt.Println("tree-started")
	_ = child.Wait()
}
