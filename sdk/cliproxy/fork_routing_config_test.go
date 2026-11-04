package cliproxy

import (
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
)

func TestRoutingConfigResetSoonestAndReclaim(t *testing.T) {
	cfg := &config.Config{}
	cfg.Routing.Strategy = "reset-soonest"
	state := normalizedRoutingRuntimeState(cfg)
	if _, ok := newRoutingSelector(state).(*coreauth.ResetSoonestSelector); !ok {
		t.Fatalf("strategy reset-soonest built %T", newRoutingSelector(state))
	}

	cfg.Routing.SessionAffinityReclaim = true
	if normalizedRoutingRuntimeState(cfg).sessionAffinityReclaim {
		t.Fatal("reclaim must be ignored without session affinity")
	}
	cfg.Routing.SessionAffinity = true
	state = normalizedRoutingRuntimeState(cfg)
	if !state.sessionAffinityReclaim {
		t.Fatal("reclaim not carried into runtime state")
	}
	if _, ok := newRoutingSelector(state).(*coreauth.SessionAffinitySelector); !ok {
		t.Fatalf("session affinity built %T", newRoutingSelector(state))
	}
	// A reclaim toggle must count as a routing change so hot reload swaps the selector.
	cfg.Routing.SessionAffinityReclaim = false
	if normalizedRoutingRuntimeState(cfg) == state {
		t.Fatal("reclaim change not detected by runtime state comparison")
	}
}
