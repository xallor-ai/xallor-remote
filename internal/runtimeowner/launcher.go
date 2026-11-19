package runtimeowner

import "context"

// Attempt exists once a worker process was created, even when its acceptance
// reply is lost. Callers retain the launch gate until this owned process exits.
type Attempt interface {
	Done() <-chan struct{}
	Result() error
}
type Launcher func(context.Context, Command) (Attempt, error)
