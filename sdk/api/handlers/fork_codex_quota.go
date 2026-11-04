package handlers

import (
	"math"
	"net/http"
	"strconv"
	"time"

	"github.com/MoeForge/moego/ojson"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
)

const codexFiveHourWindowSeconds = int64(5 * 60 * 60)

// BuildErrorResponseBodyWithError builds an OpenAI-compatible JSON error response body,
// preserving structured classifications present in err. Codex plan-quota
// exhaustion is reported as usage_limit_reached with resets_at so Codex
// clients stop instead of retrying.
func BuildErrorResponseBodyWithError(status int, errText string, err error) []byte {
	body := buildErrorResponseBodyWithError(status, errText, err)
	resetAt, _, ok := codexUsageLimitReset(status, err)
	if !ok {
		return body
	}
	doc := ojson.NewDoc(body)
	if errSet := doc.Set("error.type", "usage_limit_reached"); errSet != nil {
		return body
	}
	if errSet := doc.Set("error.resets_at", resetAt); errSet != nil {
		return body
	}
	return doc.Bytes()
}

// CodexUsageLimitHeaders synthesizes the Codex rate-limit headers for
// Codex plan-quota exhaustion, or returns nil for any other error. They are
// generated locally, so they are sent regardless of header passthrough.
func CodexUsageLimitHeaders(status int, err error) http.Header {
	resetAt, after, ok := codexUsageLimitReset(status, err)
	if !ok {
		return nil
	}
	resetAtText := strconv.FormatInt(resetAt, 10)
	afterText := strconv.FormatInt(after, 10)
	window := "300"
	if after > codexFiveHourWindowSeconds {
		window = "10080"
	}
	headers := http.Header{
		"x-codex-primary-used-percent":        {"100"},
		"x-codex-primary-reset-at":            {resetAtText},
		"x-codex-primary-reset-after-seconds": {afterText},
		"x-codex-primary-window-minutes":      {window},
	}
	if after > codexFiveHourWindowSeconds {
		// A reset beyond the five-hour window is the weekly limit.
		headers["x-codex-secondary-used-percent"] = []string{"100"}
		headers["x-codex-secondary-reset-at"] = []string{resetAtText}
		headers["x-codex-secondary-reset-after-seconds"] = []string{afterText}
		headers["x-codex-secondary-window-minutes"] = []string{"10080"}
	}
	return headers
}

func codexUsageLimitReset(status int, err error) (resetAt int64, after int64, ok bool) {
	resetIn, ok := coreauth.CodexUsageLimitResetIn(status, err)
	if !ok {
		return 0, 0, false
	}
	after = int64(math.Ceil(resetIn.Seconds()))
	if after < 0 {
		after = 0
	}
	return time.Now().Unix() + after, after, true
}
