package auth

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"testing"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/registry"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
)

const forkCreditLowBody = `{"type":"error","error":{"type":"invalid_request_error","message":"Your credit balance is too low to access the Anthropic API. Please go to Plans & Billing to upgrade or purchase credits."}}`

type forkQuotaTestError struct{ retryAfter time.Duration }

func (e forkQuotaTestError) Error() string {
	return `{"type":"error","error":{"type":"rate_limit_error","message":"usage limit reached"}}`
}
func (e forkQuotaTestError) StatusCode() int            { return http.StatusTooManyRequests }
func (e forkQuotaTestError) RetryAfter() *time.Duration { return &e.retryAfter }

func registerForkCreditAuths(t *testing.T, m *Manager, model string, auths ...*Auth) {
	t.Helper()
	reg := registry.GetGlobalRegistry()
	for _, auth := range auths {
		reg.RegisterClient(auth.ID, auth.Provider, []*registry.ModelInfo{{ID: model}})
		authID := auth.ID
		t.Cleanup(func() { reg.UnregisterClient(authID) })
		if _, errRegister := m.Register(context.Background(), auth); errRegister != nil {
			t.Fatalf("register %s: %v", auth.ID, errRegister)
		}
	}
}

func TestManager_APIKeyCreditBalanceTooLowIsNotCooledDown(t *testing.T) {
	const provider = "claude"
	const model = "claude-sonnet-4-6-fork-credit-nocool"
	m := NewManager(nil, nil, nil)
	m.SetRetryConfig(0, 0, 0)
	apiKey := &Auth{ID: "fork-credit-nocool-key", Provider: provider, Attributes: map[string]string{AttributeAPIKey: "test-key", "priority": "-1"}}
	executor := &authFallbackExecutor{id: provider, executeErrors: map[string]error{
		apiKey.ID: &Error{HTTPStatus: http.StatusBadRequest, Message: forkCreditLowBody},
	}}
	m.RegisterExecutor(executor)
	registerForkCreditAuths(t, m, model, apiKey)

	for i := 0; i < 2; i++ {
		_, errExecute := m.Execute(context.Background(), []string{provider}, cliproxyexecutor.Request{Model: model}, cliproxyexecutor.Options{})
		if errExecute == nil || !isCreditBalanceTooLowError(errExecute) {
			t.Fatalf("attempt %d: error = %v, want original credit balance error", i, errExecute)
		}
		var cooldown *modelCooldownError
		if errors.As(errExecute, &cooldown) {
			t.Fatalf("attempt %d: no OAuth candidate, error must stay unchanged: %v", i, errExecute)
		}
	}
	if calls := executor.ExecuteCalls(); !slices.Equal(calls, []string{apiKey.ID, apiKey.ID}) {
		t.Fatalf("calls = %v, want the API key retried on every request", calls)
	}
	updated, _ := m.GetByID(apiKey.ID)
	if state := updated.ModelStates[model]; state != nil && (state.Unavailable || !state.NextRetryAfter.IsZero()) {
		t.Fatalf("API key model state cooled: unavailable=%v next=%v", state.Unavailable, state.NextRetryAfter)
	}

	// OAuth credentials keep the existing 30-minute bench.
	oauth := &Auth{ID: "fork-credit-oauth", Provider: provider, Attributes: map[string]string{AttributeAuthKind: AuthKindOAuth}}
	now := time.Now()
	applyAuthFailureState(oauth, &Error{HTTPStatus: http.StatusBadRequest, Message: forkCreditLowBody}, nil, now, false)
	if oauth.NextRetryAfter.Before(now.Add(29 * time.Minute)) {
		t.Fatalf("OAuth cooldown = %v, want about 30 minutes", oauth.NextRetryAfter)
	}
	skipped := &Auth{ID: "fork-credit-key-auth-level", Provider: provider, Attributes: map[string]string{AttributeAPIKey: "k"}}
	if !skipAPIKeyCreditCooldown(skipped, &Error{HTTPStatus: http.StatusBadRequest, Message: forkCreditLowBody}) {
		t.Fatal("API-key credit balance failure must skip cooldown")
	}
}

func TestManager_APIKeyCreditLowReturnsSoonestSubscriptionCooldown(t *testing.T) {
	const provider = "claude"
	for _, stream := range []bool{false, true} {
		name := "execute"
		if stream {
			name = "stream"
		}
		t.Run(name, func(t *testing.T) {
			model := "claude-sonnet-4-6-fork-credit-sub-" + name
			m := NewManager(nil, nil, nil)
			m.SetRetryConfig(0, 0, 0)
			oauthSoon := &Auth{ID: "fork-sub-" + name + "-a", Provider: provider, Attributes: map[string]string{AttributeAuthKind: AuthKindOAuth}}
			oauthLate := &Auth{ID: "fork-sub-" + name + "-b", Provider: provider, Attributes: map[string]string{AttributeAuthKind: AuthKindOAuth}}
			apiKey := &Auth{ID: "fork-sub-" + name + "-z-key", Provider: provider, Attributes: map[string]string{AttributeAPIKey: "test-key", "priority": "-1"}}
			errs := map[string]error{
				oauthSoon.ID: forkQuotaTestError{retryAfter: 2 * time.Hour},
				oauthLate.ID: forkQuotaTestError{retryAfter: 5 * time.Hour},
				apiKey.ID:    &Error{HTTPStatus: http.StatusBadRequest, Message: forkCreditLowBody},
			}
			executor := &authFallbackExecutor{id: provider, executeErrors: errs, streamFirstErrors: errs}
			m.RegisterExecutor(executor)
			registerForkCreditAuths(t, m, model, oauthSoon, oauthLate, apiKey)

			var errRun error
			if stream {
				result, errStream := m.ExecuteStream(context.Background(), []string{provider}, cliproxyexecutor.Request{Model: model}, cliproxyexecutor.Options{})
				errRun = errStream
				if errRun == nil && result != nil {
					for chunk := range result.Chunks {
						if chunk.Err != nil {
							errRun = chunk.Err
						}
					}
				}
			} else {
				_, errRun = m.Execute(context.Background(), []string{provider}, cliproxyexecutor.Request{Model: model}, cliproxyexecutor.Options{})
			}
			var cooldown *modelCooldownError
			if !errors.As(errRun, &cooldown) {
				t.Fatalf("error = %T %v, want subscription model cooldown", errRun, errRun)
			}
			if cooldown.StatusCode() != http.StatusTooManyRequests || cooldown.provider != provider {
				t.Fatalf("cooldown status=%d provider=%q", cooldown.StatusCode(), cooldown.provider)
			}
			if cooldown.resetIn < 2*time.Hour-time.Minute || cooldown.resetIn > 2*time.Hour {
				t.Fatalf("resetIn = %v, want the soonest OAuth recovery (~2h)", cooldown.resetIn)
			}
			if got := cooldown.Headers().Get("Retry-After"); got == "" {
				t.Fatal("missing Retry-After")
			}
		})
	}
}
