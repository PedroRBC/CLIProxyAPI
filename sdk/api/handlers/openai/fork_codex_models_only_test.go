package openai

import (
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/registry"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/api/handlers"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
)

func TestCodexClientModelsCodexModelsOnly(t *testing.T) {
	const (
		codexModel  = "fork-only-codex-model"
		claudeModel = "fork-only-claude-model"
		mixedModel  = "fork-only-mixed-model"
	)
	modelRegistry := registry.GetGlobalRegistry()
	modelRegistry.RegisterClient("fork-only-codex", "codex", []*registry.ModelInfo{{ID: codexModel}, {ID: mixedModel}})
	modelRegistry.RegisterClient("fork-only-claude", "claude", []*registry.ModelInfo{{ID: claudeModel}, {ID: mixedModel}})
	t.Cleanup(func() {
		modelRegistry.UnregisterClient("fork-only-codex")
		modelRegistry.UnregisterClient("fork-only-claude")
	})

	slugs := func(codexOnly bool) map[string]bool {
		t.Helper()
		cfg := &config.SDKConfig{Client: config.ClientConfig{Codex: config.CodexClientConfig{CodexModelsOnly: codexOnly}}}
		handler := NewOpenAIAPIHandler(handlers.NewBaseAPIHandlers(cfg, auth.NewManager(nil, nil, nil)))
		found := map[string]bool{}
		for _, entry := range handler.codexClientModelsResponse("0.153.4")["models"].([]map[string]any) {
			if slug, _ := entry["slug"].(string); slug != "" {
				found[slug] = true
			}
		}
		return found
	}

	all := slugs(false)
	for _, id := range []string{codexModel, claudeModel, mixedModel} {
		if !all[id] {
			t.Fatalf("default catalog missing %q", id)
		}
	}
	only := slugs(true)
	if !only[codexModel] || !only[mixedModel] {
		t.Fatalf("codex-models-only dropped a codex-served model: %v", only)
	}
	if only[claudeModel] {
		t.Fatalf("codex-models-only kept non-codex model %q", claudeModel)
	}
}
