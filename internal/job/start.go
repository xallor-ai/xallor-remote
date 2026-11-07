package job

// beginStart keeps this ledger's file-lock ownership through OS startup and
// publication of the child to its Owner. Lock order is launch, then mu. Close
// never takes an Owner lock or waits for process cleanup while holding mu.
func (l *Ledger) beginStart(id string) (func(), error) {
	l.launch.RLock()
	if err := l.prepareStart(id); err != nil {
		l.launch.RUnlock()
		return nil, err
	}
	return l.launch.RUnlock, nil
}

// prepareStart crosses the persistent launch boundary before process.Start.
// A failed or ambiguous write never permits a launch. Once occupied, the job
// stays occupied across sequential probes and its main process until Finish.
func (l *Ledger) prepareStart(id string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed || l.err != nil {
		return ErrUnavailable
	}
	r, ok := l.jobs[id]
	if !ok {
		return ErrUnavailable
	}
	switch r.State {
	case "occupied":
		return nil
	case "reserved":
		r.State = "occupied"
		jobs := l.copyLocked()
		jobs[id] = r
		return l.saveLocked(jobs)
	default:
		return ErrUnavailable
	}
}
