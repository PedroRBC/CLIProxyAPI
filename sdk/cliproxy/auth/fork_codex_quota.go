package auth

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/MoeForge/moego/ojson"
)

// CodexUsageLimitResetIn reports whether err is Codex plan-quota exhaustion
// and how long until the soonest reset. Two shapes qualify: a model cooldown
// whose provider is codex (every Codex credential cooling), and an upstream
// Codex 429 whose body type is usage_limit_reached with a known reset.
// Ordinary throttling, overload and auth failures are never reported.
func CodexUsageLimitResetIn(status int, err error) (time.Duration, bool) {
	if err == nil || status != http.StatusTooManyRequests {
		return 0, false
	}
	var cooldown *modelCooldownError
	if errors.As(err, &cooldown) && cooldown != nil {
		return cooldown.resetIn, strings.EqualFold(strings.TrimSpace(cooldown.provider), "codex")
	}
	body := []byte(strings.TrimSpace(err.Error()))
	quota := ojson.GetBytesNoCopy(body, "error")
	if !strings.EqualFold(strings.TrimSpace(quota.Get("type").String()), "usage_limit_reached") {
		return 0, false
	}
	if hint := trustedRetryAfterHint(err); hint != nil && *hint > 0 {
		return *hint, true
	}
	if resetsAt := quota.Get("resets_at").Int(); resetsAt > 0 {
		if remaining := time.Until(time.Unix(resetsAt, 0)); remaining > 0 {
			return remaining, true
		}
	}
	if seconds := quota.Get("resets_in_seconds").Int(); seconds > 0 {
		return time.Duration(seconds) * time.Second, true
	}
	return 0, false
}
