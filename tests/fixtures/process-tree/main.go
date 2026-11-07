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
	if os.Args[1] != "parent" && os.Args[1] != "orphan" {
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
	if err = os.WriteFile(filepath.Join(dir, "pids.tmp"), data, 0600); err != nil {
		panic(err)
	}
	if err = os.Rename(filepath.Join(dir, "pids.tmp"), filepath.Join(dir, "pids.json")); err != nil {
		panic(err)
	}
	fmt.Println("tree-started")
	if os.Args[1] == "orphan" {
		deadline := time.Now().Add(10 * time.Second)
		for {
			if _, err := os.Stat(filepath.Join(dir, "heartbeat")); err == nil {
				return
			}
			if time.Now().After(deadline) {
				panic("child did not start")
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
	_ = child.Wait()
}
