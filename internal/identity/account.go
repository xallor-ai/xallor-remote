package identity

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"github.com/xallor-ai/xallor-remote/internal/atomicfile"
	"github.com/xallor-ai/xallor-remote/internal/principal"
)

func (s *Store) loadAccount() error {
	raw, err := os.ReadFile(filepath.Join(s.Dir, "account.json"))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	account, err := decodeAccountCredential(raw)
	if err != nil {
		return err
	}
	if (account.Token == "") != (account.Relay == "") || (account.Token != "" && !principal.ValidToken(account.Token)) {
		return errors.New("身份凭据无效，请重新导入")
	}
	s.account = account
	return nil
}

// Credentials pins an installed credential to its configured Relay. A changed
// endpoint cannot leak that token or silently downgrade an authenticated caller.
func (s *Store) Credentials() (relay, token string, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.accountErr != nil {
		return "", "", s.accountErr
	}
	if s.account.Token != "" && s.account.Relay != s.Config.RelayURL {
		return "", "", errors.New("Relay 已变更，请清除或重新导入身份凭据")
	}
	return s.Config.RelayURL, s.account.Token, nil
}

func (s *Store) SetAccount(token string) error {
	if token != "" && !principal.ValidToken(token) {
		return errors.New("身份凭据无效")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	next := accountCredential{}
	if token != "" {
		next = accountCredential{Relay: s.Config.RelayURL, Token: token}
	}
	raw, err := json.Marshal(next)
	if err != nil {
		return err
	}
	if err := atomicfile.Write(filepath.Join(s.Dir, "account.json"), raw); err != nil {
		return err
	}
	s.account = next
	s.accountErr = nil
	return nil
}

func (s *Store) AccountConfigured() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.account.Token != "" || s.accountErr != nil
}
