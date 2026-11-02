package release

import (
	"encoding/json"
	"errors"
	"github.com/xallor-ai/xallor-remote/internal/buildinfo"
	"github.com/xallor-ai/xallor-remote/internal/filelock"
	"os"
	"path/filepath"
)

type Store struct{ Dir string }
type State struct {
	Current  *Manifest `json:"current,omitempty"`
	Previous *Manifest `json:"previous,omitempty"`
	Pending  *Manifest `json:"pending,omitempty"`
}

func (s Store) Lock() (func(), error) {
	if err := os.MkdirAll(s.Dir, 0700); err != nil {
		return nil, err
	}
	return filelock.Acquire(filepath.Join(s.Dir, "install.lock"))
}
func (s Store) Path(m Manifest) string {
	return filepath.Join(s.Dir, "versions", m.Artifact.SHA256, m.Artifact.Filename)
}
func (s Store) Read() (State, error) {
	var state State
	err := readJSON(filepath.Join(s.Dir, "state.json"), &state)
	if os.IsNotExist(err) {
		return state, nil
	}
	if err != nil {
		return state, err
	}
	for _, m := range []*Manifest{state.Current, state.Previous, state.Pending} {
		if m != nil {
			if err = m.validate(); err != nil {
				return state, err
			}
		}
	}
	return state, nil
}
func (s Store) save(state State) error {
	b, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(s.Dir, "state-*")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	_, err = f.Write(b)
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(name, filepath.Join(s.Dir, "state.json"))
}

// ActivePath fails closed on corrupt state/artifacts rather than launching a fallback.
func (s Store) ActivePath() (string, error) {
	st, err := s.Read()
	if err != nil {
		return "", err
	}
	if st.Current == nil {
		if st.Pending != nil {
			return "", errors.New("interrupted initial install; run runtime rollback")
		}
		return "", nil
	}
	if err = s.Verify(*st.Current); err != nil {
		return "", err
	}
	return s.Path(*st.Current), nil
}
func (s Store) snapshot(e buildinfo.Executable) (Manifest, error) {
	st, err := os.Stat(e.Path)
	if err != nil {
		return Manifest{}, err
	}
	name := "xallor-remote"
	if e.OS == "windows" {
		name += ".exe"
	}
	m := Manifest{Schema: 1, Build: e.Info, Artifact: Artifact{Filename: name, SHA256: e.SHA256, Size: st.Size()}}
	if err = m.validate(); err != nil {
		return m, err
	}
	f, err := os.Open(e.Path)
	if err != nil {
		return m, err
	}
	defer f.Close()
	return m, s.copyArtifact(f, m)
}
