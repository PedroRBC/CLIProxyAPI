package auth

import (
	"net/http"
	"testing"
	"time"
)

func TestCodexUsageLimitResetIn_AllCodexCredentialsCooling(t *testing.T) {
	const routeModel = "gpt-5.5"
	now := time.Now()
	soonest := now.Add(40 * time.Minute)
	cooling := func(id, provider string, next time.Time) *Auth {
		return &Auth{
			ID:       id,
			Provider: provider,
			ModelStates: map[string]*ModelState{
				routeModel: {Status: StatusError, Unavailable: true, NextRetryAfter: next, Quota: QuotaState{Exceeded: true, Reason: "quota", NextRecoverAt: next}},
			},
		}
	}
	m := NewManager(nil, nil, nil)

	_, err := m.availableAuthsForRouteModel([]*Auth{cooling("codex-a", "codex", now.Add(3*time.Hour)), cooling("codex-b", "codex", soonest)}, "codex", routeModel, now)
	resetIn, ok := CodexUsageLimitResetIn(http.StatusTooManyRequests, err)
	if !ok {
		t.Fatalf("codex cooldown not recognised: %v", err)
	}
	if diff := resetIn - soonest.Sub(now); diff < -time.Second || diff > time.Second {
		t.Fatalf("resetIn = %v, want %v", resetIn, soonest.Sub(now))
	}

	_, err = m.availableAuthsForRouteModel([]*Auth{cooling("claude-a", "claude", soonest)}, "claude", routeModel, now)
	if _, ok := CodexUsageLimitResetIn(http.StatusTooManyRequests, err); ok {
		t.Fatalf("non-codex cooldown must not be converted: %v", err)
	}
}

func TestCodexUsageLimitResetIn_UpstreamUsageLimitOnly(t *testing.T) {
	usage := &Error{HTTPStatus: http.StatusTooManyRequests, Message: `{"error":{"type":"usage_limit_reached","message":"The usage limit has been reached","resets_in_seconds":120}}`}
	hint := 2 * time.Hour
	resetIn, ok := CodexUsageLimitResetIn(http.StatusTooManyRequests, withTrustedRetryAfter(usage, &hint))
	if !ok || resetIn < hint-time.Second || resetIn > hint {
		t.Fatalf("trusted usage limit = %v %v, want about %v", resetIn, ok, hint)
	}
	if resetIn, ok = CodexUsageLimitResetIn(http.StatusTooManyRequests, usage); !ok || resetIn != 2*time.Minute {
		t.Fatalf("body usage limit = %v %v, want 2m", resetIn, ok)
	}

	for name, err := range map[string]error{
		"generic rate limit": &Error{HTTPStatus: http.StatusTooManyRequests, Message: `{"error":{"type":"rate_limit_error","code":"rate_limit_exceeded","message":"slow down"}}`},
		"auth unavailable":   newAuthUnavailableError(time.Now().Add(time.Hour), time.Now()),
		"unknown reset":      &Error{HTTPStatus: http.StatusTooManyRequests, Message: `{"error":{"type":"usage_limit_reached"}}`},
	} {
		if _, ok := CodexUsageLimitResetIn(http.StatusTooManyRequests, err); ok {
			t.Fatalf("%s must not be converted", name)
		}
	}
	if _, ok := CodexUsageLimitResetIn(http.StatusServiceUnavailable, usage); ok {
		t.Fatal("non-429 status must not be converted")
	}
}
