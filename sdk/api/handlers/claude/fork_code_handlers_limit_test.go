package claude

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/MoeForge/moego/ojson"
	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/api/handlers"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
)

type claudeLimitTestError struct {
	status     int
	retryAfter *time.Duration
}

func (e claudeLimitTestError) Error() string {
	return `{"type":"error","error":{"type":"rate_limit_error","message":"This request would exceed your account's rate limit."}}`
}
func (e claudeLimitTestError) StatusCode() int            { return e.status }
func (e claudeLimitTestError) RetryAfter() *time.Duration { return e.retryAfter }

func writeClaudeLimitError(t *testing.T, userAgent string, err error) (*httptest.ResponseRecorder, []byte) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	c.Request.Header.Set("User-Agent", userAgent)
	h := NewClaudeCodeAPIHandler(&handlers.BaseAPIHandler{})
	msg := handlers.ExecutionErrorMessage(err)
	original, errMarshal := json.Marshal(h.toClaudeError(msg))
	if errMarshal != nil {
		t.Fatalf("marshal original: %v", errMarshal)
	}
	h.WriteErrorResponse(c, msg)
	return recorder, original
}

func TestClaudeCodeLongRateLimitBecomesResumable503(t *testing.T) {
	const userAgent = "claude-cli/2.1.90 (external, cli)"
	cases := []struct {
		name string
		err  error
		want string
	}{
		{"trusted retry after", claudeLimitTestError{status: http.StatusTooManyRequests, retryAfter: durationPtr(2*time.Hour + 59*time.Minute + 30*time.Second + 400*time.Millisecond)}, "2h59m30s"},
		{"model cooldown retry-after header", coreauth.NewModelCooldownError("claude-opus-4-7", "claude", 3*time.Hour), "3h0m0s"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			recorder, original := writeClaudeLimitError(t, userAgent, tc.err)
			if recorder.Code != http.StatusServiceUnavailable {
				t.Fatalf("status = %d, want 503", recorder.Code)
			}
			if got := recorder.Header().Get("x-should-retry"); got != "false" {
				t.Fatalf("x-should-retry = %q, want false", got)
			}
			body := recorder.Body.Bytes()
			if ojson.GetBytes(body, "type").String() != "error" {
				t.Fatalf("body = %s", body)
			}
			want := "[cliproxy] [429]: " + string(original) + " (reset after " + tc.want + ")"
			if got := ojson.GetBytes(body, "error.message").String(); got != want {
				t.Fatalf("message = %q\nwant      %q", got, want)
			}
			if ojson.GetBytes(original, "error.type").String() != "rate_limit_error" {
				t.Fatalf("original envelope = %s", original)
			}
		})
	}

	recorder, _ := writeClaudeLimitError(t, userAgent, coreauth.NewModelCooldownError("claude-opus-4-7", "claude", 3*time.Hour))
	if recorder.Header().Get("Retry-After") != "10800" {
		t.Fatalf("Retry-After = %q, want 10800", recorder.Header().Get("Retry-After"))
	}
}

func TestClaudeCodeRateLimitKeepsBehaviorOutsideResumableCase(t *testing.T) {
	cases := []struct {
		name       string
		userAgent  string
		err        error
		wantStatus int
	}{
		{"short wait", "claude-cli/2.1.90", claudeLimitTestError{status: http.StatusTooManyRequests, retryAfter: durationPtr(4 * time.Minute)}, http.StatusTooManyRequests},
		{"unknown reset", "claude-cli/2.1.90", claudeLimitTestError{status: http.StatusTooManyRequests}, http.StatusTooManyRequests},
		{"other user agent", "t3code/1.0", coreauth.NewModelCooldownError("claude-opus-4-7", "claude", 3*time.Hour), http.StatusTooManyRequests},
		{"non 429", "claude-cli/2.1.90", claudeLimitTestError{status: http.StatusServiceUnavailable, retryAfter: durationPtr(time.Hour)}, http.StatusServiceUnavailable},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			recorder, original := writeClaudeLimitError(t, tc.userAgent, tc.err)
			if recorder.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d", recorder.Code, tc.wantStatus)
			}
			if recorder.Header().Get("x-should-retry") != "" {
				t.Fatal("unexpected x-should-retry header")
			}
			if recorder.Body.String() != string(original) {
				t.Fatalf("body = %s, want unchanged %s", recorder.Body.String(), original)
			}
		})
	}
	if _, ok := claudeLimitResetIn(errors.New("plain")); ok {
		t.Fatal("plain error must have no reset")
	}
}

func durationPtr(d time.Duration) *time.Duration { return &d }
