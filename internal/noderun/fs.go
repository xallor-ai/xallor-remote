package noderun

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/xallor-ai/xallor-remote/internal/protocol"
)

func workspaceRel(workspace, p string) (string, error) {
	if strings.TrimSpace(p) == "" {
		return "", fmt.Errorf("empty path")
	}
	if len(p) > protocol.MaxPathBytes {
		return "", fmt.Errorf("path too long")
	}
	ws, err := filepath.Abs(filepath.Clean(workspace))
	if err != nil {
		return "", err
	}
	target := p
	if !filepath.IsAbs(target) {
		target = filepath.Join(ws, p)
	}
	target, err = filepath.Abs(filepath.Clean(target))
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(ws, target)
	if err != nil {
		return "", err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) || !filepath.IsLocal(rel) && rel != "." {
		return "", fmt.Errorf("outside workspace")
	}
	if denySensitivePath(target) {
		return "", fmt.Errorf("sensitive path")
	}
	return rel, nil
}

func openWorkspace(workspace string) (*os.Root, error) {
	ws, err := filepath.Abs(filepath.Clean(workspace))
	if err != nil {
		return nil, err
	}
	return os.OpenRoot(ws)
}

func resolveInWorkspace(workspace, p string) (string, error) {
	rel, err := workspaceRel(workspace, p)
	if err != nil {
		return "", err
	}
	root, err := openWorkspace(workspace)
	if err != nil {
		return "", err
	}
	defer root.Close()
	if _, err := root.Stat(rel); err != nil {
		if !os.IsNotExist(err) {
			return "", err
		}
		parent := filepath.Dir(rel)
		if parent != "." {
			if _, perr := root.Stat(parent); perr != nil && !os.IsNotExist(perr) {
				return "", perr
			}
		}
	}
	ws, err := filepath.Abs(filepath.Clean(workspace))
	if err != nil {
		return "", err
	}
	return filepath.Join(ws, rel), nil
}

func statWorkspaceFile(workspace, p string) (os.FileInfo, error) {
	rel, err := workspaceRel(workspace, p)
	if err != nil {
		return nil, err
	}
	root, err := openWorkspace(workspace)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	return root.Stat(rel)
}

func readWorkspaceFile(workspace, p string) ([]byte, error) {
	rel, err := workspaceRel(workspace, p)
	if err != nil {
		return nil, err
	}
	root, err := openWorkspace(workspace)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	f, err := root.Open(rel)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(f)
}

func writeWorkspaceFile(workspace, p, content string) error {
	rel, err := workspaceRel(workspace, p)
	if err != nil {
		return err
	}
	root, err := openWorkspace(workspace)
	if err != nil {
		return err
	}
	defer root.Close()
	parent := filepath.Dir(rel)
	if parent != "." {
		if _, err := root.Stat(parent); err != nil {
			if !os.IsNotExist(err) {
				return err
			}
			if err := root.MkdirAll(parent, 0o700); err != nil {
				return err
			}
		}
	}
	tmpRel := filepath.Join(parent, fmt.Sprintf(".xr-write-%d", time.Now().UnixNano()))
	f, err := root.OpenFile(tmpRel, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	_, werr := f.Write([]byte(content))
	cerr := f.Close()
	if werr != nil || cerr != nil {
		_ = root.Remove(tmpRel)
		if werr != nil {
			return werr
		}
		return cerr
	}
	if err := root.Rename(tmpRel, rel); err != nil {
		_ = root.Remove(tmpRel)
		return err
	}
	return nil
}
