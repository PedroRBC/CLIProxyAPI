package auth

import (
	"context"
	"strconv"
	"strings"
	"time"

	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
)

// weeklyWindowMinutes identifies the weekly window among Codex quota slots.
const weeklyWindowMinutes = 7 * 24 * 60

// ResetSoonestSelector picks, among the highest available priority tier, the credential
// whose weekly quota window resets first, so the allowance closest to expiring is spent
// before it is wiped. Reset times come from the rate-limit headers each credential last
// returned (Claude unified 7d, Codex weekly slot). Credentials without a known future
// reset (unused since start, or other providers) rotate round-robin only when no
// credential in the tier has one.
type ResetSoonestSelector struct {
	fallback RoundRobinSelector
}

// Pick implements Selector.
func (s *ResetSoonestSelector) Pick(ctx context.Context, provider, model string, opts cliproxyexecutor.Options, auths []*Auth) (*Auth, error) {
	now := time.Now()
	available, err := getSelectorAvailableAuths(ctx, auths, provider, model, now)
	if err != nil {
		return nil, err
	}
	available = preferCodexWebsocketAuths(ctx, provider, available)
	var (
		best   *Auth
		bestAt time.Time
	)
	for _, auth := range available {
		at, ok := weeklyResetAt(auth, now)
		if !ok {
			continue
		}
		if best == nil || at.Before(bestAt) || (at.Equal(bestAt) && auth.ID < best.ID) {
			best, bestAt = auth, at
		}
	}
	if best != nil {
		return best, nil
	}
	return s.fallback.Pick(ctx, provider, model, opts, available)
}

// weeklyResetAt returns the future weekly reset instant observed for a credential.
func weeklyResetAt(auth *Auth, now time.Time) (time.Time, bool) {
	if auth == nil || len(auth.Quota.Signals) == 0 {
		return time.Time{}, false
	}
	signals := auth.Quota.Signals
	var at time.Time
	if unix, ok := signalInt(signals, "Anthropic-Ratelimit-Unified-7d-Reset"); ok {
		at = time.Unix(unix, 0)
	} else {
		for _, slot := range []string{"Primary", "Secondary"} {
			if minutes, ok := signalInt(signals, "X-Codex-"+slot+"-Window-Minutes"); !ok || minutes != weeklyWindowMinutes {
				continue
			}
			if unix, ok := signalInt(signals, "X-Codex-"+slot+"-Reset-At"); ok {
				at = time.Unix(unix, 0)
			} else if after, ok := signalInt(signals, "X-Codex-"+slot+"-Reset-After-Seconds"); ok && !auth.Quota.ObservedAt.IsZero() {
				at = auth.Quota.ObservedAt.Add(time.Duration(after) * time.Second)
			}
			break
		}
	}
	if at.IsZero() || !at.After(now) {
		return time.Time{}, false
	}
	return at, true
}

func signalInt(signals map[string]string, key string) (int64, bool) {
	raw := strings.TrimSpace(signals[key])
	if raw == "" {
		return 0, false
	}
	// Some upstreams send fractional seconds.
	if dot := strings.IndexByte(raw, '.'); dot > 0 {
		raw = raw[:dot]
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	return value, err == nil && value > 0
}
