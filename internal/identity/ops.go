package identity

import (
	"os"
	"path/filepath"

	"github.com/xallor-ai/xallor-remote/internal/protocol"
)

func (s *Store) RotateGrant() (string, error) {
	return s.IssueGrant()
}

func (s *Store) SetInbound(on bool) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	next := s.device
	if on && next.Grant == "" {
		var err error
		next.Grant, err = protocol.NewGrant()
		if err != nil {
			return "", err
		}
	}
	next.Inbound = on
	if err := s.publishDevice(next); err != nil {
		return "", err
	}
	return next.Grant, nil
}

func (s *Store) ClearGrant() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	next := s.device
	next.Grant, next.Inbound = "", false
	return s.publishDevice(next)
}

func (s *Store) RemovePeer(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := s.Peers[:0]
	for _, p := range s.Peers {
		if p.DeviceID != id {
			out = append(out, p)
		}
	}
	s.Peers = out
	return s.persistPeers()
}

func (s *Store) Wipe() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.publishDevice(deviceRecord{}); err != nil {
		return err
	}
	if err := finishProfileReset(s.Dir); err != nil {
		return err
	}
	s.Peers = nil
	s.Config = Config{}
	s.account = accountCredential{}
	s.accountErr = nil
	return nil
}

// The inactive database row remains a recovery marker until these ancillary
// files are cleared. Startup must finish cleanup before creating a new identity.
func finishProfileReset(dir string) error {
	for _, name := range []string{"peers.json", "config.json", "account.json"} {
		if err := os.Remove(filepath.Join(dir, name)); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}
