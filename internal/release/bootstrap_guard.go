package release

// WithBootstrapGuard lets the owner serialize bootstrap with lifecycle request
// admission without coupling release selection to the transaction database.
// Fresh source proof is checked first; candidate work then holds lifecycle before
// install.lock. Existing-owner read-only observations do not need this guard.
func (s Store) WithBootstrapGuard(guard func(func() error) error) Store {
	s.bootstrapGuard = guard
	return s
}
