package executor

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
)

func TestCodexUsageLimitStatusErrIsReportedAsPlanQuota(t *testing.T) {
	resetsAt := time.Now().Add(4 * time.Hour).Unix()
	body := []byte(fmt.Sprintf(`{"error":{"type":"usage_limit_reached","message":"The usage limit has been reached","plan_type":"plus","resets_at":%d}}`, resetsAt))
	err := newCodexStatusErrWithHeadersAndCooling(http.StatusTooManyRequests, http.Header{"Retry-After": {"30"}}, body, false)

	resetIn, ok := cliproxyauth.CodexUsageLimitResetIn(err.StatusCode(), err)
	if !ok {
		t.Fatalf("usage_limit_reached not recognised: %v", err)
	}
	if want := time.Until(time.Unix(resetsAt, 0)); resetIn < want-2*time.Second || resetIn > want+2*time.Second {
		t.Fatalf("resetIn = %v, want about %v (body reset, not Retry-After)", resetIn, want)
	}

	for name, other := range map[string][]byte{
		"turn rate limit":    []byte(issue7017TurnRateLimitBody),
		"generic rate limit": []byte(`{"error":{"type":"rate_limit_error","code":"rate_limit_exceeded","message":"Rate limit reached"}}`),
		"server overloaded":  []byte(`{"error":{"type":"server_error","code":"server_is_overloaded","message":"overloaded"}}`),
	} {
		statusErr := newCodexStatusErrWithHeadersAndCooling(http.StatusTooManyRequests, http.Header{"Retry-After": {"30"}}, other, false)
		if _, ok := cliproxyauth.CodexUsageLimitResetIn(statusErr.StatusCode(), statusErr); ok {
			t.Fatalf("%s must not be reported as plan quota", name)
		}
	}
}
