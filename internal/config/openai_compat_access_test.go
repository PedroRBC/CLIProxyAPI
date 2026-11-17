package config

import (
	"testing"

	"gopkg.in/yaml.v3"
)

func TestOpenAICompatibilityAllowedAPIKeysYAMLDecoding(t *testing.T) {
	var cfg Config
	if err := yaml.Unmarshal([]byte(`openai-compatibility:
  - name: kimi
    allowed-api-keys: [client-kimi]
`), &cfg); err != nil {
		t.Fatalf("yaml decode error: %v", err)
	}
	if got := cfg.OpenAICompatibility[0].AllowedAPIKeys; len(got) != 1 || got[0] != "client-kimi" {
		t.Fatalf("AllowedAPIKeys = %#v, want [client-kimi]", got)
	}
}
