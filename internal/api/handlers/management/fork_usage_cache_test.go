package management

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
)

const testClaudeUsageURL = "http://api.anthropic.com/api/oauth/usage"

type claudeUsageFixture struct {
	t        *testing.T
	router   *gin.Engine
	upstream *httptest.Server
	hits     atomic.Int64
	status   atomic.Int64
	now      time.Time
	indexes  []string
}

// newClaudeUsageFixture serves upstream through an HTTP proxy so the request
// URL keeps the api.anthropic.com host without leaving the test process.
func newClaudeUsageFixture(t *testing.T) *claudeUsageFixture {
	t.Helper()
	f := &claudeUsageFixture{t: t, now: time.Unix(1_800_000_000, 0)}
	f.status.Store(http.StatusOK)
	f.upstream = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := f.hits.Add(1)
		w.WriteHeader(int(f.status.Load()))
		_, _ = fmt.Fprintf(w, "%s#%d", r.URL.Path, n)
	}))
	t.Cleanup(f.upstream.Close)

	manager := coreauth.NewManager(nil, &coreauth.RoundRobinSelector{}, nil)
	for _, id := range []string{"claude-a.json", "claude-b.json"} {
		a := &coreauth.Auth{ID: id, Provider: "claude", Metadata: map[string]any{"type": "claude", "access_token": "token-" + id}}
		if _, errRegister := manager.Register(context.Background(), a); errRegister != nil {
			t.Fatalf("register %s: %v", id, errRegister)
		}
		f.indexes = append(f.indexes, a.EnsureIndex())
	}
	h := &Handler{cfg: &config.Config{}, authManager: manager}
	h.claudeUsage.nowFunc = func() time.Time { return f.now }
	f.router = gin.New()
	f.router.POST("/", h.APICall)
	return f
}

func (f *claudeUsageFixture) call(authIndex, method, rawURL string) apiCallResponse {
	f.t.Helper()
	header := map[string]string{}
	if authIndex != "" {
		header["Authorization"] = "Bearer $TOKEN$"
	}
	payload, _ := json.Marshal(map[string]any{
		"auth_index": authIndex,
		"method":     method,
		"url":        rawURL,
		"proxy_url":  f.upstream.URL,
		"header":     header,
	})
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(string(payload)))
	req.Header.Set("Content-Type", "application/json")
	f.router.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusOK {
		f.t.Fatalf("APICall status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	var resp apiCallResponse
	if errDecode := json.Unmarshal(recorder.Body.Bytes(), &resp); errDecode != nil {
		f.t.Fatalf("decode: %v", errDecode)
	}
	return resp
}

func (f *claudeUsageFixture) wantHits(want int64) {
	f.t.Helper()
	if got := f.hits.Load(); got != want {
		f.t.Fatalf("upstream hits = %d, want %d", got, want)
	}
}

func TestClaudeUsageCacheServesWithinTTLAndRefetchesAfter(t *testing.T) {
	f := newClaudeUsageFixture(t)
	first := f.call(f.indexes[0], http.MethodGet, testClaudeUsageURL)
	f.now = f.now.Add(claudeUsageCacheTTL - time.Second)
	second := f.call(f.indexes[0], http.MethodGet, testClaudeUsageURL)
	f.wantHits(1)
	if second.Body != first.Body || second.StatusCode != http.StatusOK {
		t.Fatalf("cached response = %+v, want %+v", second, first)
	}
	f.now = f.now.Add(time.Second)
	if third := f.call(f.indexes[0], http.MethodGet, testClaudeUsageURL); third.Body == first.Body {
		t.Fatalf("expected a refetch after TTL, got %q", third.Body)
	}
	f.wantHits(2)
}

func TestClaudeUsageCacheReplaces429WithRecentGoodResult(t *testing.T) {
	f := newClaudeUsageFixture(t)
	good := f.call(f.indexes[0], http.MethodGet, testClaudeUsageURL)
	f.status.Store(http.StatusTooManyRequests)

	f.now = f.now.Add(claudeUsageStaleTTL - time.Second)
	if got := f.call(f.indexes[0], http.MethodGet, testClaudeUsageURL); got.StatusCode != http.StatusOK || got.Body != good.Body {
		t.Fatalf("429 within stale window = %+v, want cached %+v", got, good)
	}
	f.wantHits(2)

	f.now = f.now.Add(time.Second)
	if got := f.call(f.indexes[0], http.MethodGet, testClaudeUsageURL); got.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("429 after stale window status = %d, want 429", got.StatusCode)
	}
	f.wantHits(3)
}

func TestClaudeUsageCacheIsPerCredential(t *testing.T) {
	f := newClaudeUsageFixture(t)
	a := f.call(f.indexes[0], http.MethodGet, testClaudeUsageURL)
	b := f.call(f.indexes[1], http.MethodGet, testClaudeUsageURL)
	f.wantHits(2)
	if a.Body == b.Body {
		t.Fatalf("credentials shared a cache entry: %q", a.Body)
	}
}

func TestClaudeUsageCacheSkipsOtherRequests(t *testing.T) {
	f := newClaudeUsageFixture(t)
	requests := []struct{ authIndex, method, url string }{
		{f.indexes[0], http.MethodPost, testClaudeUsageURL},
		{f.indexes[0], http.MethodGet, "http://api.anthropic.com/api/oauth/profile"},
		{f.indexes[0], http.MethodGet, "http://example.com/api/oauth/usage"},
		{"", http.MethodGet, testClaudeUsageURL},
	}
	var want int64
	for _, r := range requests {
		f.call(r.authIndex, r.method, r.url)
		f.call(r.authIndex, r.method, r.url)
		want += 2
		f.wantHits(want)
	}

	// Non-2xx results are never cached either.
	f.status.Store(http.StatusInternalServerError)
	f.call(f.indexes[1], http.MethodGet, testClaudeUsageURL)
	f.call(f.indexes[1], http.MethodGet, testClaudeUsageURL)
	f.wantHits(want + 2)
}

func TestClaudeUsageCacheCoalescesConcurrentMisses(t *testing.T) {
	var cache claudeUsageCache
	var calls atomic.Int64
	release := make(chan struct{})
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			resp, errGet := cache.get("k", func() (apiCallResponse, error) {
				calls.Add(1)
				<-release
				return apiCallResponse{StatusCode: http.StatusOK, Body: "ok"}, nil
			})
			if errGet != nil || resp.Body != "ok" {
				t.Errorf("get = %+v, %v", resp, errGet)
			}
		}()
	}
	close(release)
	wg.Wait()
	if got := calls.Load(); got != 1 {
		t.Fatalf("fetch calls = %d, want 1", got)
	}
}
