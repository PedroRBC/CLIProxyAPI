package configaccess

import (
	"context"
	"net/http"
	"strings"
	"sync"

	proxyconfig "github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/util"
	sdkaccess "github.com/router-for-me/CLIProxyAPI/v8/sdk/access"
	sdkconfig "github.com/router-for-me/CLIProxyAPI/v8/sdk/config"
)

var inlineProvider *provider
var registeredCompatTypes []string

// Register ensures the config-access provider is available to the access manager.
func Register(cfg *sdkconfig.SDKConfig) {
	if cfg == nil {
		sdkaccess.UnregisterProvider(sdkaccess.AccessProviderTypeConfigAPIKey)
		inlineProvider = nil
		return
	}

	keys := normalizeKeys(cfg.APIKeys)
	if len(keys) == 0 {
		sdkaccess.UnregisterProvider(sdkaccess.AccessProviderTypeConfigAPIKey)
		inlineProvider = nil
		return
	}

	inlineProvider = newProvider(sdkaccess.DefaultAccessProviderName, keys)
	sdkaccess.RegisterProvider(
		sdkaccess.AccessProviderTypeConfigAPIKey,
		inlineProvider,
	)
}

// RegisterOpenAICompatibility registers client keys restricted to individual
// OpenAI-compatible providers.
func RegisterOpenAICompatibility(cfg *proxyconfig.Config) {
	for _, typ := range registeredCompatTypes {
		sdkaccess.UnregisterProvider(typ)
	}
	registeredCompatTypes = nil
	restricted := make(map[string]struct{})
	providerKeys := make(map[string][]string)
	providerOrder := make([]string, 0)
	if cfg != nil {
		for _, compat := range cfg.OpenAICompatibility {
			providerName := strings.ToLower(strings.TrimSpace(compat.Name))
			if providerName == "" {
				providerName = "openai-compatibility"
			}
			providerName = util.OpenAICompatibleProviderKey(providerName)
			keys := normalizeKeys(compat.AllowedAPIKeys)
			if len(keys) == 0 {
				continue
			}
			for _, key := range keys {
				restricted[key] = struct{}{}
			}
			if _, exists := providerKeys[providerName]; !exists {
				providerOrder = append(providerOrder, providerName)
			}
			providerKeys[providerName] = append(providerKeys[providerName], keys...)
		}
		for _, providerName := range providerOrder {
			keys := providerKeys[providerName]
			typ := "config-api-key:" + providerName
			sdkaccess.RegisterProvider(typ, newProvider(providerName, normalizeKeys(keys)))
			registeredCompatTypes = append(registeredCompatTypes, typ)
		}
	}
	if inlineProvider != nil {
		inlineProvider.setRestricted(restricted)
	}
}

type provider struct {
	name       string
	keys       map[string]struct{}
	restricted map[string]struct{}
	mu         sync.RWMutex
}

func (p *provider) setRestricted(keys map[string]struct{}) {
	if p == nil {
		return
	}
	p.mu.Lock()
	p.restricted = keys
	p.mu.Unlock()
}

func newProvider(name string, keys []string) *provider {
	providerName := strings.TrimSpace(name)
	if providerName == "" {
		providerName = sdkaccess.DefaultAccessProviderName
	}
	keySet := make(map[string]struct{}, len(keys))
	for _, key := range keys {
		keySet[key] = struct{}{}
	}
	return &provider{name: providerName, keys: keySet}
}

func (p *provider) Identifier() string {
	if p == nil || p.name == "" {
		return sdkaccess.DefaultAccessProviderName
	}
	return p.name
}

func (p *provider) Authenticate(_ context.Context, r *http.Request) (*sdkaccess.Result, *sdkaccess.AuthError) {
	if p == nil {
		return nil, sdkaccess.NewNotHandledError()
	}
	p.mu.RLock()
	restricted := p.restricted
	p.mu.RUnlock()
	if len(p.keys) == 0 {
		return nil, sdkaccess.NewNotHandledError()
	}
	authHeader := r.Header.Get("Authorization")
	authHeaderGoogle := r.Header.Get("X-Goog-Api-Key")
	authHeaderAnthropic := r.Header.Get("X-Api-Key")
	queryKey := ""
	queryAuthToken := ""
	if r.URL != nil {
		queryKey = r.URL.Query().Get("key")
		queryAuthToken = r.URL.Query().Get("auth_token")
	}
	if authHeader == "" && authHeaderGoogle == "" && authHeaderAnthropic == "" && queryKey == "" && queryAuthToken == "" {
		return nil, sdkaccess.NewNoCredentialsError()
	}

	apiKey := extractBearerToken(authHeader)

	candidates := []struct {
		value  string
		source string
	}{
		{apiKey, "authorization"},
		{authHeaderGoogle, "x-goog-api-key"},
		{authHeaderAnthropic, "x-api-key"},
		{queryKey, "query-key"},
		{queryAuthToken, "query-auth-token"},
	}

	for _, candidate := range candidates {
		if candidate.value == "" {
			continue
		}
		if _, ok := p.keys[candidate.value]; ok {
			if _, restricted := restricted[candidate.value]; restricted {
				return nil, sdkaccess.NewNotHandledError()
			}
			return &sdkaccess.Result{
				Provider:  p.Identifier(),
				Principal: candidate.value,
				Metadata: map[string]string{
					"source": candidate.source,
				},
			}, nil
		}
	}

	return nil, sdkaccess.NewInvalidCredentialError()
}

func extractBearerToken(header string) string {
	if header == "" {
		return ""
	}
	parts := strings.SplitN(header, " ", 2)
	if len(parts) != 2 {
		return header
	}
	if strings.ToLower(parts[0]) != "bearer" {
		return header
	}
	return strings.TrimSpace(parts[1])
}

func normalizeKeys(keys []string) []string {
	if len(keys) == 0 {
		return nil
	}
	normalized := make([]string, 0, len(keys))
	seen := make(map[string]struct{}, len(keys))
	for _, key := range keys {
		trimmedKey := strings.TrimSpace(key)
		if trimmedKey == "" {
			continue
		}
		if _, exists := seen[trimmedKey]; exists {
			continue
		}
		seen[trimmedKey] = struct{}{}
		normalized = append(normalized, trimmedKey)
	}
	if len(normalized) == 0 {
		return nil
	}
	return normalized
}
