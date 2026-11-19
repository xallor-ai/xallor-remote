package accesscode

import (
	"errors"
	"sort"
)

var ErrSessionScope = errors.New("session scope or code generation is not authorized")

// ScopeForSession returns a detached canonical subset. It never silently
// expands an empty request or substitutes a newer generation of the code.
func (c Code) ScopeForSession(generation uint64, requested []string) ([]string, error) {
	if generation == 0 || generation != c.Generation || len(requested) == 0 {
		return nil, ErrSessionScope
	}
	allowed := make(map[string]bool, len(c.Scope))
	for _, op := range c.Scope {
		allowed[op] = true
	}
	seen := make(map[string]bool, len(requested))
	scope := make([]string, 0, len(requested))
	for _, op := range requested {
		if op == "" || !allowed[op] {
			return nil, ErrSessionScope
		}
		if !seen[op] {
			seen[op] = true
			scope = append(scope, op)
		}
	}
	sort.Strings(scope)
	return scope, nil
}
