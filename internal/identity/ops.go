package identity

import (
	"os"
	"path/filepath"
)

func (s *Store) RotateGrant() (string, error) {
	return s.IssueGrant()
}

func (s *Store) SetInbound(on bool) (string, error) {
	if on {
		s.mu.Lock()
		has := s.Grant != ""
		s.mu.Unlock()
		if !has {
			return s.IssueGrant()
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Inbound = on
	return s.Grant, s.persistIdentity()
}

func (s *Store) ClearGrant() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Grant = ""
	s.Inbound = false
	if err := s.persistGrant(); err != nil {
		return err
	}
	return s.persistIdentity()
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
	for _, name := range []string{"identity.json", "device_secret", "grant", "peers.json", "config.json", "account.json"} {
		_ = os.Remove(filepath.Join(s.Dir, name))
	}
	s.DeviceID = ""
	s.Secret = ""
	s.Grant = ""
	s.Inbound = false
	s.Peers = nil
	s.Config = Config{}
	s.account = accountCredential{}
	s.accountErr = nil
	return nil
}
