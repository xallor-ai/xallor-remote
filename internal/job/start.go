package job

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
