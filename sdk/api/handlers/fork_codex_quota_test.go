package handlers

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/MoeForge/moego/ojson"
	"github.com/gin-gonic/gin"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
)

type codexQuotaStatusTestError struct {
	status int
	body   string
}

func (e codexQuotaStatusTestError) Error() string   { return e.body }
func (e codexQuotaStatusTestError) StatusCode() int { return e.status }

func writeCodexQuotaTestError(t *testing.T, err error) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	NewBaseAPIHandlers(nil, nil).WriteErrorResponse(c, ExecutionErrorMessage(err))
	return recorder
}

func assertNear(t *testing.T, name string, got string, want int64) {
	t.Helper()
	value, errParse := strconv.ParseInt(got, 10, 64)
	if errParse != nil || value < want-2 || value > want+2 {
		t.Fatalf("%s = %q, want about %d", name, got, want)
	}
}

func TestWriteErrorResponse_CodexModelCooldownBecomesUsageLimit(t *testing.T) {
	now := time.Now().Unix()
	recorder := writeCodexQuotaTestError(t, coreauth.NewModelCooldownError("gpt-5.5", "codex", 2*time.Hour))

	if recorder.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429", recorder.Code)
	}
	body := recorder.Body.Bytes()
	if got := ojson.GetBytes(body, "error.type").String(); got != "usage_limit_reached" {
		t.Fatalf("error.type = %q, body %s", got, body)
	}
	if got := ojson.GetBytes(body, "error.code").String(); got != "model_cooldown" {
		t.Fatalf("error.code = %q, want model_cooldown", got)
	}
	assertNear(t, "error.resets_at", ojson.GetBytes(body, "error.resets_at").String(), now+7200)
	header := recorder.Header()
	if header.Get("Retry-After") != "7200" {
		t.Fatalf("Retry-After = %q, want 7200", header.Get("Retry-After"))
	}
	if header.Get("X-Codex-Primary-Used-Percent") != "100" || header.Get("X-Codex-Primary-Window-Minutes") != "300" {
		t.Fatalf("primary headers = %v", header)
	}
	assertNear(t, "primary reset-at", header.Get("X-Codex-Primary-Reset-At"), now+7200)
	assertNear(t, "primary reset-after", header.Get("X-Codex-Primary-Reset-After-Seconds"), 7200)
	if header.Get("X-Codex-Secondary-Used-Percent") != "" {
		t.Fatalf("secondary headers must be absent within five hours: %v", header)
	}
}

func TestWriteErrorResponse_CodexWeeklyCooldownEmitsSecondaryHeaders(t *testing.T) {
	now := time.Now().Unix()
	recorder := writeCodexQuotaTestError(t, coreauth.NewModelCooldownError("gpt-5.5", "codex", 72*time.Hour))
	header := recorder.Header()
	for _, prefix := range []string{"X-Codex-Primary-", "X-Codex-Secondary-"} {
		if header.Get(prefix+"Used-Percent") != "100" || header.Get(prefix+"Window-Minutes") != "10080" {
			t.Fatalf("%s headers = %v", prefix, header)
		}
		assertNear(t, prefix+"Reset-At", header.Get(prefix+"Reset-At"), now+72*3600)
		assertNear(t, prefix+"Reset-After-Seconds", header.Get(prefix+"Reset-After-Seconds"), 72*3600)
	}
}

func TestWriteErrorResponse_CodexUpstreamUsageLimitGetsHeaders(t *testing.T) {
	resetsAt := time.Now().Add(90 * time.Minute).Unix()
	err := codexQuotaStatusTestError{
		status: http.StatusTooManyRequests,
		body:   fmt.Sprintf(`{"error":{"type":"usage_limit_reached","message":"The usage limit has been reached","resets_at":%d}}`, resetsAt),
	}
	recorder := writeCodexQuotaTestError(t, err)
	if recorder.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429", recorder.Code)
	}
	body := recorder.Body.Bytes()
	if ojson.GetBytes(body, "error.type").String() != "usage_limit_reached" {
		t.Fatalf("body = %s", body)
	}
	assertNear(t, "error.resets_at", ojson.GetBytes(body, "error.resets_at").String(), resetsAt)
	assertNear(t, "primary reset-at", recorder.Header().Get("X-Codex-Primary-Reset-At"), resetsAt)
	if recorder.Header().Get("X-Codex-Primary-Window-Minutes") != "300" {
		t.Fatalf("window = %q, want 300", recorder.Header().Get("X-Codex-Primary-Window-Minutes"))
	}
}

func TestWriteErrorResponse_NonCodexQuotaErrorsUnchanged(t *testing.T) {
	cases := map[string]error{
		"claude cooldown":       coreauth.NewModelCooldownError("claude-sonnet-4-6", "claude", time.Hour),
		"mixed cooldown":        coreauth.NewModelCooldownError("gpt-5.5", "", time.Hour),
		"codex rate limit":      codexQuotaStatusTestError{status: http.StatusTooManyRequests, body: `{"error":{"type":"rate_limit_error","code":"rate_limit_exceeded","message":"slow down"}}`},
		"usage limit wrong 503": codexQuotaStatusTestError{status: http.StatusServiceUnavailable, body: `{"error":{"type":"usage_limit_reached","resets_in_seconds":60}}`},
		"usage limit no reset":  codexQuotaStatusTestError{status: http.StatusTooManyRequests, body: `{"error":{"type":"usage_limit_reached","message":"limit"}}`},
		"plain overload":        errors.New("overloaded"),
	}
	for name, err := range cases {
		t.Run(name, func(t *testing.T) {
			recorder := writeCodexQuotaTestError(t, err)
			if got := recorder.Header().Get("X-Codex-Primary-Used-Percent"); got != "" {
				t.Fatalf("unexpected codex header %q", got)
			}
			if ojson.GetBytes(recorder.Body.Bytes(), "error.resets_at").Exists() {
				t.Fatalf("unexpected resets_at in %s", recorder.Body.String())
			}
			if name != "usage limit no reset" && name != "usage limit wrong 503" && ojson.GetBytes(recorder.Body.Bytes(), "error.type").String() == "usage_limit_reached" {
				t.Fatalf("unexpected usage_limit_reached in %s", recorder.Body.String())
			}
		})
	}
}
