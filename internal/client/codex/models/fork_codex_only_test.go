package models

import "testing"

func TestFilterCodexServedModels(t *testing.T) {
	providers := map[string][]string{
		"gpt-x":    {"codex"},
		"claude-x": {"claude"},
		"mixed":    {"claude", "Codex"},
	}
	providersForModel := func(id string) []string { return providers[id] }
	models := []map[string]any{{"id": "gpt-x"}, {"id": "claude-x"}, {"id": "mixed"}, {"id": "team/gpt-x"}, {"id": "unknown"}}

	got := FilterCodexServedModels(models, providersForModel)
	var ids []string
	for _, model := range got {
		ids = append(ids, model["id"].(string))
	}
	want := []string{"gpt-x", "mixed", "team/gpt-x"}
	if len(ids) != len(want) {
		t.Fatalf("ids = %v, want %v", ids, want)
	}
	for i := range want {
		if ids[i] != want[i] {
			t.Fatalf("ids = %v, want %v", ids, want)
		}
	}
	if len(FilterCodexServedModels(models, nil)) != 0 {
		t.Fatal("nil provider lookup must keep nothing")
	}
}
