package auth

// reclaimable reports whether session-affinity reclaim is enabled and the bound
// credential sits below the highest usable priority, so its binding should be
// released and the next pick should move back up. The bound credential's priority
// is read from the usable list first and from all candidates otherwise, so a
// lower-tier binding that is only transiently unavailable is not preserved either.
func (s *SessionAffinitySelector) reclaimable(boundID string, candidates, available []*Auth) bool {
	if s == nil || !s.reclaim || boundID == "" || len(available) == 0 {
		return false
	}
	best := authPriority(highestPriorityAuths(available)[0])
	for _, list := range [][]*Auth{available, candidates} {
		for _, auth := range list {
			if auth != nil && auth.ID == boundID {
				return authPriority(auth) < best
			}
		}
	}
	return false
}
