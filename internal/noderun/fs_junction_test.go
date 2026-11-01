package noderun

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

func makeJunction(t *testing.T, link, target string) {
	t.Helper()
	out, err := exec.Command("cmd", "/c", "mklink", "/J", link, target).CombinedOutput()
	if err != nil {
		t.Fatalf("mklink /J: %v %s", err, out)
	}
	t.Cleanup(func() { _ = exec.Command("cmd", "/c", "rmdir", link).Run() })
}

func outsideListing(t *testing.T, dir string) []string {
	t.Helper()
	ents, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(ents))
	for _, e := range ents {
		names = append(names, e.Name())
	}
	return names
}

// junction / `..` 必须 deny；工作区外哨兵不得被读、截断或新增。
func TestResolveInWorkspace_should_deny_when_junction_escapes(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("junction is Windows-only")
	}
	ws := t.TempDir()
	outside := t.TempDir()
	sentinel := filepath.Join(outside, "secret.txt")
	if err := os.WriteFile(sentinel, []byte("SENTINEL_OUTSIDE"), 0o600); err != nil {
		t.Fatal(err)
	}
	junc := filepath.Join(ws, "junc")
	makeJunction(t, junc, outside)

	if _, err := resolveInWorkspace(ws, filepath.Join(ws, "..", "nope.txt")); err == nil {
		t.Fatal("dot-dot should deny")
	}
	if _, err := resolveInWorkspace(ws, filepath.Join("junc", "secret.txt")); err == nil {
		t.Fatal("existing file through junction should deny")
	}
	if _, err := resolveInWorkspace(ws, filepath.Join("junc", "newfile.txt")); err == nil {
		t.Fatal("new file through junction should deny")
	}
	got, err := os.ReadFile(sentinel)
	if err != nil || string(got) != "SENTINEL_OUTSIDE" {
		t.Fatalf("sentinel mutated: %q err=%v", got, err)
	}
}

func TestReadWriteWorkspace_should_leave_sentinel_when_junction_escapes(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("junction is Windows-only")
	}
	ws := t.TempDir()
	outside := t.TempDir()
	sentinel := filepath.Join(outside, "secret.txt")
	if err := os.WriteFile(sentinel, []byte("SENTINEL_OUTSIDE"), 0o600); err != nil {
		t.Fatal(err)
	}
	makeJunction(t, filepath.Join(ws, "junc"), outside)

	if _, err := readWorkspaceFile(ws, filepath.Join("junc", "secret.txt")); err == nil {
		t.Fatal("read through junction should deny")
	}
	if err := writeWorkspaceFile(ws, filepath.Join("junc", "secret.txt"), "HACKED"); err == nil {
		t.Fatal("overwrite through junction should deny")
	}
	if err := writeWorkspaceFile(ws, filepath.Join("junc", "newfile.txt"), "HACKED"); err == nil {
		t.Fatal("create through junction should deny")
	}

	got, err := os.ReadFile(sentinel)
	if err != nil || string(got) != "SENTINEL_OUTSIDE" {
		t.Fatalf("sentinel mutated: %q err=%v", got, err)
	}
	if _, err := os.Stat(filepath.Join(outside, "newfile.txt")); !os.IsNotExist(err) {
		t.Fatalf("outside gained newfile: %v", err)
	}
	if names := outsideListing(t, outside); len(names) != 1 || names[0] != "secret.txt" {
		t.Fatalf("outside listing changed: %v", names)
	}
}

// 解析后把目录换成指向外部的 junction，写入不得落到工作区外。
func TestWriteWorkspace_should_deny_when_dir_replaced_with_junction(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("junction is Windows-only")
	}
	ws := t.TempDir()
	outside := t.TempDir()
	sentinel := filepath.Join(outside, "secret.txt")
	if err := os.WriteFile(sentinel, []byte("SENTINEL_OUTSIDE"), 0o600); err != nil {
		t.Fatal(err)
	}
	sub := filepath.Join(ws, "sub")
	if err := os.Mkdir(sub, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "file.txt"), []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(sub); err != nil {
		t.Fatal(err)
	}
	makeJunction(t, sub, outside)

	if err := writeWorkspaceFile(ws, filepath.Join("sub", "file.txt"), "HACKED"); err == nil {
		t.Fatal("write after junction replace should deny")
	}
	if err := writeWorkspaceFile(ws, filepath.Join("sub", "secret.txt"), "HACKED"); err == nil {
		t.Fatal("replace onto outside sentinel should deny")
	}
	got, err := os.ReadFile(sentinel)
	if err != nil || string(got) != "SENTINEL_OUTSIDE" {
		t.Fatalf("sentinel mutated: %q err=%v", got, err)
	}
	if _, err := os.Stat(filepath.Join(outside, "file.txt")); !os.IsNotExist(err) {
		t.Fatalf("replace landed outside: %v", err)
	}
}
