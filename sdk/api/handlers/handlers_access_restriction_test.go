package handlers

import (
	"context"
	"net/http"
	"testing"

	sdkaccess "github.com/router-for-me/CLIProxyAPI/v8/sdk/access"
	coreusage "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/usage"
)

type accessRestrictionTestProvider struct{}

func (accessRestrictionTestProvider) Identifier() string { return "openai-compatible-venice" }

func (accessRestrictionTestProvider) Authenticate(context.Context, *http.Request) (*sdkaccess.Result, *sdkaccess.AuthError) {
	return nil, sdkaccess.NewNotHandledError()
}

func TestFilterRestrictedProviders(t *testing.T) {
	sdkaccess.RegisterProvider("access-restriction-test", accessRestrictionTestProvider{})
	t.Cleanup(func() { sdkaccess.UnregisterProvider("access-restriction-test") })

	ordinary := coreusage.WithAccessProvider(context.Background(), "config-inline")
	providers, errMsg := filterRestrictedProviders(ordinary, []string{"openai-compatible-venice", "openai-compatible-mistral"})
	if errMsg != nil {
		t.Fatalf("ordinary key returned error: %+v", errMsg)
	}
	if len(providers) != 1 || providers[0] != "openai-compatible-mistral" {
		t.Fatalf("ordinary key providers = %#v, want only unrestricted provider", providers)
	}

	allowed := coreusage.WithAccessProvider(context.Background(), "openai-compatible-venice")
	providers, errMsg = filterRestrictedProviders(allowed, []string{"openai-compatible-venice", "openai-compatible-mistral"})
	if errMsg != nil {
		t.Fatalf("restricted key returned error: %+v", errMsg)
	}
	if len(providers) != 2 {
		t.Fatalf("restricted key providers = %#v, want full provider list", providers)
	}
}

func TestFilterRestrictedProvidersRejectsOnlyRestrictedModel(t *testing.T) {
	sdkaccess.RegisterProvider("access-restriction-only-test", accessRestrictionTestProvider{})
	t.Cleanup(func() { sdkaccess.UnregisterProvider("access-restriction-only-test") })

	ordinary := coreusage.WithAccessProvider(context.Background(), "config-inline")
	providers, errMsg := filterRestrictedProviders(ordinary, []string{"openai-compatible-venice"})
	if errMsg == nil || providers != nil || errMsg.StatusCode != http.StatusForbidden {
		t.Fatalf("ordinary key result = providers=%#v error=%+v, want forbidden", providers, errMsg)
	}
}
