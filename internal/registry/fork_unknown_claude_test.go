package registry

import (
	"reflect"
	"testing"
)

func TestUnregisteredClaudeModelRouting(t *testing.T) {
	r := newTestModelRegistry()
	if got := r.UnregisteredClaudeProviders("claude-next-9"); got != nil {
		t.Fatalf("without Claude credentials = %v, want nil", got)
	}

	r.RegisterClient("claude-1", "claude", []*ModelInfo{{ID: "claude-known"}})
	r.RegisterClient("codex-1", "codex", []*ModelInfo{{ID: "gpt-x"}, {ID: "claude-on-codex"}})

	if got := r.UnregisteredClaudeProviders("claude-next-9"); !reflect.DeepEqual(got, []string{"claude"}) {
		t.Fatalf("unregistered claude-* = %v, want [claude]", got)
	}
	if got := r.UnregisteredClaudeProviders("Claude-Next-9"); !reflect.DeepEqual(got, []string{"claude"}) {
		t.Fatalf("case-insensitive prefix = %v, want [claude]", got)
	}
	for _, model := range []string{"claude-known", "claude-on-codex", "gpt-next", "my-claude-x"} {
		if got := r.UnregisteredClaudeProviders(model); got != nil {
			t.Fatalf("%s = %v, want nil (registered or not claude-*)", model, got)
		}
	}

	if !r.ClientSupportsModel("claude-1", "claude-next-9") {
		t.Fatal("Claude credential must accept an unregistered claude-* model")
	}
	if r.ClientSupportsModel("codex-1", "claude-next-9") {
		t.Fatal("non-Claude credential must not accept an unregistered claude-* model")
	}
	// Registered models keep upstream routing: a Claude credential that does not list a
	// model another provider registers is still rejected.
	if r.ClientSupportsModel("claude-1", "claude-on-codex") {
		t.Fatal("registered model must keep normal support checks")
	}
}
