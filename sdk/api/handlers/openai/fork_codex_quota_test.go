package openai

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/MoeForge/moego/ojson"
	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/api/handlers"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
	sdkconfig "github.com/router-for-me/CLIProxyAPI/v8/sdk/config"
)

func TestResponsesInitialStreamErrorCodexCooldownBecomesUsageLimit(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	h := NewOpenAIResponsesAPIHandler(handlers.NewBaseAPIHandlers(&sdkconfig.SDKConfig{}, nil))

	errMsg := handlers.ExecutionErrorMessage(coreauth.NewModelCooldownError("gpt-5.5", "codex", 6*time.Hour))
	// Same steps as the pre-commit SSE branch of the Responses handler.
	h.WriteErrorResponse(c, sanitizeResponsesInitialErrorMessage(errMsg))

	if recorder.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429", recorder.Code)
	}
	body := recorder.Body.Bytes()
	if ojson.GetBytes(body, "error.type").String() != "usage_limit_reached" || !ojson.GetBytes(body, "error.resets_at").Exists() {
		t.Fatalf("body = %s", body)
	}
	header := recorder.Header()
	if header.Get("X-Codex-Primary-Window-Minutes") != "10080" || header.Get("X-Codex-Secondary-Window-Minutes") != "10080" {
		t.Fatalf("headers = %v", header)
	}
}

func TestResponsesWebsocketErrorPayloadCodexCooldownIncludesHeaders(t *testing.T) {
	errMsg := handlers.ExecutionErrorMessage(coreauth.NewModelCooldownError("gpt-5.5", "codex", time.Hour))
	payload, errBuild := buildResponsesWebsocketErrorPayload(errMsg)
	if errBuild != nil {
		t.Fatalf("build payload: %v", errBuild)
	}
	if got := ojson.GetBytes(payload, "status").Int(); got != http.StatusTooManyRequests {
		t.Fatalf("status = %d, payload %s", got, payload)
	}
	if ojson.GetBytes(payload, "error.type").String() != "usage_limit_reached" || !ojson.GetBytes(payload, "error.resets_at").Exists() {
		t.Fatalf("payload = %s", payload)
	}
	if ojson.GetBytes(payload, "headers.x-codex-primary-used-percent").String() != "100" ||
		ojson.GetBytes(payload, "headers.x-codex-primary-window-minutes").String() != "300" ||
		ojson.GetBytes(payload, "headers.Retry-After").String() != "3600" {
		t.Fatalf("payload headers = %s", ojson.GetBytes(payload, "headers").Raw)
	}
	if ojson.GetBytes(payload, "headers.x-codex-secondary-used-percent").Exists() {
		t.Fatalf("secondary headers must be absent within five hours: %s", payload)
	}

	plain := handlers.ExecutionErrorMessage(coreauth.NewModelCooldownError("claude-sonnet-4-6", "claude", time.Hour))
	payload, errBuild = buildResponsesWebsocketErrorPayload(plain)
	if errBuild != nil {
		t.Fatalf("build payload: %v", errBuild)
	}
	if ojson.GetBytes(payload, "headers.x-codex-primary-used-percent").Exists() || ojson.GetBytes(payload, "error.resets_at").Exists() {
		t.Fatalf("non-codex payload changed: %s", payload)
	}
}
