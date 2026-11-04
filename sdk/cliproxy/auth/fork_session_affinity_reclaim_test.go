package auth

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/registry"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
)

func TestManagerSessionAffinityReclaimReturnsToHigherPriority(t *testing.T) {
	for _, rebindOn := range []string{SessionAffinityRebindOnAny, SessionAffinityRebindOnQuota} {
		for _, mixed := range []bool{false, true} {
			name := rebindOn + "/single"
			if mixed {
				name = rebindOn + "/mixed"
			}
			t.Run(name, func(t *testing.T) {
				ctx := context.Background()
				provider := "affinity-reclaim-" + rebindOn
				if mixed {
					provider += "-mixed"
				}
				model := "affinity-reclaim-model"
				highID := provider + "-high"
				lowID := provider + "-low"

				manager := NewManager(nil, nil, nil)
				affinity := NewSessionAffinitySelectorWithConfig(SessionAffinityConfig{
					Fallback: &RoundRobinSelector{},
					TTL:      time.Hour,
					RebindOn: rebindOn,
					Reclaim:  true,
				})
				defer affinity.Stop()
				manager.SetSelector(affinity)
				manager.RegisterExecutor(schedulerTestExecutor{provider: provider})
				for _, auth := range []*Auth{
					{ID: highID, Provider: provider, Status: StatusActive, Attributes: map[string]string{"priority": "1"}},
					{ID: lowID, Provider: provider, Status: StatusActive, Attributes: map[string]string{"priority": "0"}},
				} {
					if _, errRegister := manager.Register(WithSkipPersist(ctx), auth); errRegister != nil {
						t.Fatalf("Register(%s): %v", auth.ID, errRegister)
					}
					registry.GetGlobalRegistry().RegisterClient(auth.ID, provider, []*registry.ModelInfo{{ID: model}})
					t.Cleanup(func() { registry.GetGlobalRegistry().UnregisterClient(auth.ID) })
				}

				opts := cliproxyexecutor.Options{Metadata: map[string]any{
					cliproxyexecutor.DerivedSessionIDMetadataKey: "reclaim-session",
				}}
				pick := func() string {
					t.Helper()
					var (
						auth    *Auth
						errPick error
					)
					if mixed {
						auth, _, _, errPick = manager.pickNextMixed(ctx, []string{provider}, model, opts, nil)
					} else {
						auth, _, errPick = manager.pickNext(ctx, provider, model, opts, nil)
					}
					if errPick != nil || auth == nil {
						t.Fatalf("pick = %v (err=%v)", auth, errPick)
					}
					return auth.ID
				}

				if got := pick(); got != highID {
					t.Fatalf("cold binding = %q, want %q", got, highID)
				}
				manager.MarkResult(ctx, Result{
					AuthID: highID, Provider: provider, Model: model, Success: false,
					Error: &Error{HTTPStatus: http.StatusTooManyRequests, Message: "quota"}, Options: opts,
				})
				if got := pick(); got != lowID {
					t.Fatalf("failover = %q, want %q", got, lowID)
				}
				if got := pick(); got != lowID {
					t.Fatalf("while high cools = %q, want sticky %q", got, lowID)
				}

				expireSessionAffinityPriorityModelCooldown(t, manager, highID, model)
				if got := pick(); got != highID {
					t.Fatalf("after high recovered = %q, want reclaimed %q", got, highID)
				}
				if got := pick(); got != highID {
					t.Fatalf("after reclaim = %q, want sticky %q", got, highID)
				}
			})
		}
	}
}

func TestSessionAffinityReclaimDoesNotPreserveLowerTierBinding(t *testing.T) {
	t.Parallel()
	selector := NewSessionAffinitySelectorWithConfig(SessionAffinityConfig{
		Fallback: &FillFirstSelector{},
		RebindOn: SessionAffinityRebindOnQuota,
		Reclaim:  true,
	})
	t.Cleanup(selector.Stop)
	high := &Auth{ID: "a-high", Provider: "codex", Attributes: map[string]string{"priority": "1"}}
	low := &Auth{ID: "b-low", Provider: "codex", Attributes: map[string]string{"priority": "0"}}
	opts := cliproxyexecutor.Options{Metadata: map[string]any{
		cliproxyexecutor.DerivedSessionIDMetadataKey: "lower-tier",
	}}

	guardianTestPick(t, selector, "codex", "model", opts, []*Auth{low}, low.ID)
	// The bound lower-tier credential is only transiently down, which rebind-on quota would
	// normally preserve; reclaim must instead bind to the usable higher tier.
	low.ModelStates = map[string]*ModelState{
		"model": {Unavailable: true, NextRetryAfter: time.Now().Add(time.Minute), LastError: &Error{HTTPStatus: 503}},
	}
	guardianTestPick(t, selector, "codex", "model", opts, []*Auth{high, low}, high.ID)
	low.ModelStates = nil
	guardianTestPick(t, selector, "codex", "model", opts, []*Auth{high, low}, high.ID)
}

func TestSessionAffinityReclaimGuardianSkipsLowerTierParent(t *testing.T) {
	t.Parallel()
	for _, reclaim := range []bool{false, true} {
		selector := NewSessionAffinitySelectorWithConfig(SessionAffinityConfig{
			Fallback: &FillFirstSelector{},
			Reclaim:  reclaim,
		})
		high := &Auth{ID: "auth-a", Provider: "codex", Attributes: map[string]string{"priority": "1"}}
		low := &Auth{ID: "auth-b", Provider: "codex", Attributes: map[string]string{"priority": "0"}}
		guardianTestPick(t, selector, "codex", "parent-model", guardianTestOptions("main-thread", ""), []*Auth{low}, low.ID)
		want := low.ID
		if reclaim {
			want = high.ID
		}
		guardianTestPick(t, selector, "codex", "review-model", guardianTestOptions("review-thread", "main-thread"), []*Auth{high, low}, want)
		selector.Stop()
	}
}

func TestSessionAffinityReclaimDisabledKeepsEstablishedBinding(t *testing.T) {
	t.Parallel()
	selector := NewSessionAffinitySelectorWithConfig(SessionAffinityConfig{Fallback: &FillFirstSelector{}})
	t.Cleanup(selector.Stop)
	high := &Auth{ID: "a-high", Provider: "codex", Attributes: map[string]string{"priority": "1"}}
	low := &Auth{ID: "b-low", Provider: "codex", Attributes: map[string]string{"priority": "0"}}
	opts := cliproxyexecutor.Options{Metadata: map[string]any{
		cliproxyexecutor.DerivedSessionIDMetadataKey: "sticky",
	}}
	guardianTestPick(t, selector, "codex", "model", opts, []*Auth{low}, low.ID)
	guardianTestPick(t, selector, "codex", "model", opts, []*Auth{high, low}, low.ID)
}
