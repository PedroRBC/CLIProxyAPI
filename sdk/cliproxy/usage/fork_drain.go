package usage

import "context"

// WaitDrained blocks until the dispatcher has delivered every queued record
// after Stop, or until ctx ends. It reports whether the queue fully drained.
// A dispatcher that never started has nothing to drain.
func (m *Manager) WaitDrained(ctx context.Context) bool {
	if m == nil {
		return true
	}
	m.mu.Lock()
	done := m.done
	m.mu.Unlock()
	if done == nil {
		return true
	}
	if ctx == nil {
		ctx = context.Background()
	}
	select {
	case <-done:
		return true
	case <-ctx.Done():
		return false
	}
}

// StopDefaultAndWait stops the default manager and waits for its queue to
// drain, bounded by ctx.
func StopDefaultAndWait(ctx context.Context) bool {
	StopDefault()
	return DefaultManager().WaitDrained(ctx)
}
