package handlers

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/interfaces"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/access"
	coreusage "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/usage"
)

func restrictedOpenAICompatProvider(ctx context.Context) string {
	provider := strings.TrimSpace(coreusage.AccessProviderFromContext(ctx))
	if strings.HasPrefix(provider, "openai-compatible-") || provider == "openai-compatibility" {
		return provider
	}
	return ""
}

func filterRestrictedProviders(ctx context.Context, providers []string) ([]string, *interfaces.ErrorMessage) {
	restricted := make(map[string]struct{})
	for _, provider := range access.RegisteredProviders() {
		if provider == nil {
			continue
		}
		name := strings.ToLower(strings.TrimSpace(provider.Identifier()))
		if strings.HasPrefix(name, "openai-compatible-") {
			restricted[name] = struct{}{}
		}
	}
	if len(restricted) == 0 {
		return providers, nil
	}
	allowedProvider := strings.ToLower(strings.TrimSpace(restrictedOpenAICompatProvider(ctx)))
	filtered := make([]string, 0, len(providers))
	for _, provider := range providers {
		name := strings.TrimSpace(provider)
		if _, isRestricted := restricted[strings.ToLower(name)]; isRestricted && strings.ToLower(name) != allowedProvider {
			continue
		}
		filtered = append(filtered, provider)
	}
	if len(filtered) == 0 && len(providers) > 0 {
		return nil, &interfaces.ErrorMessage{
			StatusCode: http.StatusForbidden,
			Error:      fmt.Errorf("the requested model is restricted to an authorized OpenAI-compatible API key"),
		}
	}
	return filtered, nil
}
