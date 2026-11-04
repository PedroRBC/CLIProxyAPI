package auth

import (
	"strings"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/registry"
)

// skipAPIKeyCreditCooldown reports an API-key credential rejected for low
// credit balance. Such a key is never benched, so topped-up credit is used on
// the very next request; OAuth credentials keep the regular cooldown.
func skipAPIKeyCreditCooldown(auth *Auth, resultErr *Error) bool {
	return auth != nil && auth.AuthKind() == AuthKindAPIKey && isCreditBalanceTooLowResultError(resultErr)
}

// subscriptionCooldownForCreditLow replaces a final credit-balance-too-low
// failure of a last-resort API key with the soonest quota cooldown of the
// route's OAuth subscriptions, so clients can resume when the plan returns.
// Without a cooling OAuth candidate the original error is returned unchanged.
func (m *Manager) subscriptionCooldownForCreditLow(providers []string, routeModel string, err error) error {
	if m == nil || err == nil || !isCreditBalanceTooLowError(err) {
		return err
	}
	providerSet := make(map[string]struct{}, len(providers))
	for _, provider := range providers {
		if p := canonicalSchedulingProvider(provider); p != "" {
			providerSet[p] = struct{}{}
		}
	}
	now := time.Now()
	registryRef := registry.GetGlobalRegistry()
	apiKeyCreditLow := false
	var soonest time.Time
	soonestProvider := ""

	m.mu.RLock()
	for _, candidate := range m.auths {
		if candidate == nil || candidate.Disabled {
			continue
		}
		providerKey := executorKeyFromAuth(candidate)
		if _, ok := providerSet[providerKey]; !ok {
			continue
		}
		if strings.TrimSpace(routeModel) != "" && !m.authSupportsRouteModel(registryRef, candidate, routeModel) {
			continue
		}
		switch candidate.AuthKind() {
		case AuthKindAPIKey:
			if isCreditBalanceTooLowResultError(candidate.LastError) {
				apiKeyCreditLow = true
			}
		case AuthKindOAuth:
			blocked, reason, next := isAuthBlockedForModel(candidate, m.selectionModelForAuth(candidate, routeModel), now)
			if blocked && reason == blockReasonCooldown && next.After(now) && (soonest.IsZero() || next.Before(soonest)) {
				soonest = next
				soonestProvider = providerKey
			}
		}
	}
	m.mu.RUnlock()

	if !apiKeyCreditLow || soonest.IsZero() {
		return err
	}
	return newModelCooldownError(routeModel, soonestProvider, soonest.Sub(now))
}
