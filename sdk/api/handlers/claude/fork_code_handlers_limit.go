package claude

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/interfaces"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/runtime/executor/helps"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
)

const claudeCodeResumableLimitMinWait = 5 * time.Minute

// claudeCodeResumableLimitBody turns a long rate limit for Claude Code into a
// non-retryable 503 so the CLI stops instead of retrying for hours. The
// message text format "[cliproxy] [429]: <original envelope> (reset after
// <duration>)" is what T3 Code's Claude adapter parses to resume the session
// after the reset; keep it stable.
func claudeCodeResumableLimitBody(c *gin.Context, status int, msg *interfaces.ErrorMessage, original []byte) ([]byte, bool) {
	if status != http.StatusTooManyRequests || msg == nil || msg.Error == nil || c == nil || c.Request == nil || c.Writer.Written() {
		return nil, false
	}
	if !helps.IsClaudeCodeUserAgent(c.Request.Header.Get("User-Agent")) {
		return nil, false
	}
	resetIn, ok := claudeLimitResetIn(msg.Error)
	if !ok || resetIn < claudeCodeResumableLimitMinWait {
		return nil, false
	}
	body, errMarshal := json.Marshal(claudeErrorResponse{
		Type: "error",
		Error: claudeErrorDetail{
			Type:    "api_error",
			Message: fmt.Sprintf("[cliproxy] [429]: %s (reset after %s)", original, resetIn.Round(time.Second)),
		},
	})
	if errMarshal != nil {
		return nil, false
	}
	return body, true
}

// claudeLimitResetIn prefers the error's own retry hint (trusted cooldown
// deadline) and falls back to the locally trusted Retry-After header.
func claudeLimitResetIn(err error) (time.Duration, bool) {
	var provider interface{ RetryAfter() *time.Duration }
	if errors.As(err, &provider) && provider != nil {
		if retryAfter := provider.RetryAfter(); retryAfter != nil && *retryAfter > 0 {
			return *retryAfter, true
		}
	}
	if value := coreauth.SafeResponseHeaders(err).Get("Retry-After"); value != "" {
		if seconds, errParse := strconv.Atoi(value); errParse == nil && seconds > 0 {
			return time.Duration(seconds) * time.Second, true
		}
	}
	return 0, false
}
