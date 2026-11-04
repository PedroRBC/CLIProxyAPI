package models

import "strings"

// FilterCodexServedModels keeps only models that at least one codex provider
// serves (client.codex.codex-models-only). Prefixed aliases fall back to their
// base ID like the other provider-dependent catalog checks.
func FilterCodexServedModels(models []map[string]any, providersForModel ProvidersForModelFunc) []map[string]any {
	result := make([]map[string]any, 0, len(models))
	if providersForModel == nil {
		return result
	}
	for _, model := range models {
		id := strings.TrimSpace(stringModelValue(model, "id"))
		if id == "" {
			continue
		}
		for _, provider := range codexClientModelProviders(id, providersForModel) {
			if strings.EqualFold(strings.TrimSpace(provider), "codex") {
				result = append(result, model)
				break
			}
		}
	}
	return result
}
