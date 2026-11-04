package auth

import (
	"context"
	"testing"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/registry"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/util"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
)

func TestManagerRoutesUnregisteredClaudeModelToClaudeCredentials(t *testing.T) {
	for _, affinity := range []bool{false, true} {
		name := "scheduler"
		if affinity {
			name = "session-affinity"
		}
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			authID := "fork-unknown-claude-" + name
			unknownModel := "claude-fork-unreleased-" + name

			manager := NewManager(nil, nil, nil)
			if affinity {
				selector := NewSessionAffinitySelectorWithConfig(SessionAffinityConfig{Fallback: &RoundRobinSelector{}, TTL: time.Hour})
				defer selector.Stop()
				manager.SetSelector(selector)
			}
			manager.RegisterExecutor(schedulerTestExecutor{provider: "claude"})
			if _, errRegister := manager.Register(WithSkipPersist(ctx), &Auth{ID: authID, Provider: "claude", Status: StatusActive}); errRegister != nil {
				t.Fatalf("Register: %v", errRegister)
			}
			registry.GetGlobalRegistry().RegisterClient(authID, "claude", []*registry.ModelInfo{{ID: "claude-fork-known"}})
			t.Cleanup(func() { registry.GetGlobalRegistry().UnregisterClient(authID) })

			if got := util.GetProviderName(unknownModel); len(got) != 1 || got[0] != "claude" {
				t.Fatalf("GetProviderName(%s) = %v, want [claude]", unknownModel, got)
			}
			opts := cliproxyexecutor.Options{Metadata: map[string]any{
				cliproxyexecutor.DerivedSessionIDMetadataKey: "unknown-claude",
			}}
			auth, _, errPick := manager.pickNext(ctx, "claude", unknownModel, opts, nil)
			if errPick != nil || auth == nil || auth.ID != authID {
				t.Fatalf("pickNext(%s) = %v (err=%v), want %q", unknownModel, auth, errPick, authID)
			}
		})
	}
}
