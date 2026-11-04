package management

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
	"golang.org/x/sync/singleflight"
)

const (
	// claudeUsageCacheTTL serves a good plan-usage read without contacting upstream.
	claudeUsageCacheTTL = 2 * time.Minute
	// claudeUsageStaleTTL bounds how old a good read may be to replace an upstream 429.
	claudeUsageStaleTTL = 30 * time.Minute
)

// apiCallFailure is an APICall error that maps to a management HTTP response.
type apiCallFailure struct {
	status  int
	message string
}

func (e *apiCallFailure) Error() string { return e.message }

func writeAPICallFailure(c *gin.Context, err error) {
	if failure, ok := err.(*apiCallFailure); ok {
		c.JSON(failure.status, gin.H{"error": failure.message})
		return
	}
	c.JSON(http.StatusBadGateway, gin.H{"error": "request failed"})
}

type claudeUsageCacheEntry struct {
	resp      apiCallResponse
	fetchedAt time.Time
}

// claudeUsageCache caches Claude OAuth plan-usage reads per credential so that
// dashboards polling /api/oauth/usage do not trip Anthropic's rate limit.
type claudeUsageCache struct {
	mu      sync.Mutex
	entries map[string]claudeUsageCacheEntry
	group   singleflight.Group
	nowFunc func() time.Time
}

func (cache *claudeUsageCache) now() time.Time {
	if cache.nowFunc != nil {
		return cache.nowFunc()
	}
	return time.Now()
}

// claudeUsageCacheKey returns "" for requests that must not be cached.
func claudeUsageCacheKey(method string, u *url.URL, authIndex string, auth *coreauth.Auth) string {
	if auth == nil || u == nil || method != http.MethodGet || !strings.EqualFold(u.Hostname(), "api.anthropic.com") || u.Path != "/api/oauth/usage" {
		return ""
	}
	return authIndex + "\x00" + strconv.FormatUint(auth.CredentialVersion, 10) + "\x00" +
		strings.ToLower(u.Scheme) + "://" + strings.ToLower(u.Host) + u.Path + "?" + u.Query().Encode()
}

func (cache *claudeUsageCache) fresh(key string) (apiCallResponse, bool) {
	cache.mu.Lock()
	defer cache.mu.Unlock()
	entry, ok := cache.entries[key]
	if !ok || cache.now().Sub(entry.fetchedAt) >= claudeUsageCacheTTL {
		return apiCallResponse{}, false
	}
	return entry.resp, true
}

func (cache *claudeUsageCache) get(key string, fetch func() (apiCallResponse, error)) (apiCallResponse, error) {
	if resp, ok := cache.fresh(key); ok {
		return resp, nil
	}
	value, errDo, _ := cache.group.Do(key, func() (any, error) {
		// A flight that finished just before this one may have filled the cache.
		if resp, ok := cache.fresh(key); ok {
			return resp, nil
		}
		resp, errFetch := fetch()
		if errFetch != nil {
			return resp, errFetch
		}
		cache.mu.Lock()
		defer cache.mu.Unlock()
		now := cache.now()
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			if cache.entries == nil {
				cache.entries = make(map[string]claudeUsageCacheEntry)
			}
			for k, entry := range cache.entries {
				if now.Sub(entry.fetchedAt) >= claudeUsageStaleTTL {
					delete(cache.entries, k)
				}
			}
			cache.entries[key] = claudeUsageCacheEntry{resp: resp, fetchedAt: now}
			return resp, nil
		}
		if entry, ok := cache.entries[key]; ok && resp.StatusCode == http.StatusTooManyRequests && now.Sub(entry.fetchedAt) < claudeUsageStaleTTL {
			return entry.resp, nil
		}
		return resp, nil
	})
	resp, _ := value.(apiCallResponse)
	return resp, errDo
}

// doAPICall sends the request, through the plan-usage cache when key is set.
func (h *Handler) doAPICall(ctx context.Context, key string, fetch func(context.Context) (apiCallResponse, error)) (apiCallResponse, error) {
	if key == "" {
		return fetch(ctx)
	}
	// Coalesced callers share one fetch, so one caller leaving must not cancel it;
	// the APICall client timeout still bounds it.
	return h.claudeUsage.get(key, func() (apiCallResponse, error) {
		return fetch(context.WithoutCancel(ctx))
	})
}
