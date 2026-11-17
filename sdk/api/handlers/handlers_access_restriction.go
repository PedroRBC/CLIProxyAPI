package handlers

import (
	"context"
	"strings"

	coreusage "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/usage"
)

func restrictedOpenAICompatProvider(ctx context.Context) string {
	provider := strings.TrimSpace(coreusage.AccessProviderFromContext(ctx))
	if strings.HasPrefix(provider, "openai-compatible-") || provider == "openai-compatibility" {
		return provider
	}
	return ""
}
