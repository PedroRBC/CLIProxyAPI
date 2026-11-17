package configaccess

import (
	"net/http/httptest"
	"testing"

	proxyconfig "github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	sdkaccess "github.com/router-for-me/CLIProxyAPI/v8/sdk/access"
)

func TestProviderAuthenticatesAllowedKey(t *testing.T) {
	p := newProvider("openai-compatible-kimi", []string{"client-kimi"})
	req := httptest.NewRequest("GET", "/v1/models", nil)
	req.Header.Set("Authorization", "Bearer client-kimi")
	result, authErr := p.Authenticate(nil, req)
	if authErr != nil || result == nil {
		t.Fatalf("Authenticate() = result=%#v err=%v, want success", result, authErr)
	}
	if result.Provider != "openai-compatible-kimi" || result.Principal != "client-kimi" {
		t.Fatalf("result = %#v, want provider and principal", result)
	}
}

func TestProviderRejectsOtherKey(t *testing.T) {
	p := newProvider("openai-compatible-kimi", []string{"client-kimi"})
	req := httptest.NewRequest("GET", "/v1/models", nil)
	req.Header.Set("Authorization", "Bearer other")
	if _, authErr := p.Authenticate(nil, req); authErr == nil || authErr.Code != "invalid_credential" {
		t.Fatalf("Authenticate() error = %v, want invalid credential", authErr)
	}
}

func TestRegisterOpenAICompatibilityCreatesRestrictedAccessProvider(t *testing.T) {
	cfg := &proxyconfig.Config{}
	cfg.APIKeys = []string{"global"}
	cfg.OpenAICompatibility = []proxyconfig.OpenAICompatibility{{Name: "kimi", AllowedAPIKeys: []string{"client-kimi"}}}
	Register(&cfg.SDKConfig)
	RegisterOpenAICompatibility(cfg)
	t.Cleanup(func() {
		RegisterOpenAICompatibility(nil)
		sdkaccess.UnregisterProvider(sdkaccess.AccessProviderTypeConfigAPIKey)
	})

	manager := sdkaccess.NewManager()
	manager.SetProviders(sdkaccess.RegisteredProviders())
	req := httptest.NewRequest("GET", "/v1/models", nil)
	req.Header.Set("Authorization", "Bearer client-kimi")
	result, authErr := manager.Authenticate(nil, req)
	if authErr != nil || result == nil || result.Provider != "openai-compatible-kimi" {
		t.Fatalf("restricted authentication = result=%#v err=%v", result, authErr)
	}
}
