package registry

import "strings"

// New Claude models ship before the catalog lists them. A claude-* model that no
// credential registers is routed to every Claude credential instead of failing with
// "unknown provider for model"; registered models keep normal routing. Upstream may
// still reject a name that does not exist, which is reported to the client as usual.

// unregisteredClaudeModelLocked reports whether modelID is a claude-* name without any
// registered provider. The caller must hold r.mutex.
func (r *ModelRegistry) unregisteredClaudeModelLocked(modelID string) bool {
	trimmed := strings.TrimSpace(modelID)
	lower := strings.ToLower(trimmed)
	if !strings.HasPrefix(lower, "claude-") {
		return false
	}
	for _, key := range []string{trimmed, lower} {
		if registration := r.models[key]; registration != nil && len(registration.Providers) > 0 {
			return false
		}
	}
	return true
}

// IsUnregisteredClaudeModel reports whether modelID is a claude-* model no credential registers.
func (r *ModelRegistry) IsUnregisteredClaudeModel(modelID string) bool {
	r.mutex.RLock()
	defer r.mutex.RUnlock()
	return r.unregisteredClaudeModelLocked(modelID)
}

// UnregisteredClaudeProviders returns ["claude"] when modelID is an unregistered claude-*
// model and at least one Claude credential is registered, otherwise nil.
func (r *ModelRegistry) UnregisteredClaudeProviders(modelID string) []string {
	r.mutex.RLock()
	defer r.mutex.RUnlock()
	if !r.unregisteredClaudeModelLocked(modelID) {
		return nil
	}
	for _, provider := range r.clientProviders {
		if provider == "claude" {
			return []string{"claude"}
		}
	}
	return nil
}
