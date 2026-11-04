package management

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/redisqueue"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/usagehistory"
)

func TestUsageHistoryImportValidationAndRead(t *testing.T) {
	old := redisqueue.HistoryStore()
	t.Cleanup(func() { redisqueue.SetHistoryStore(old) })
	store, err := usagehistory.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	redisqueue.SetHistoryStore(store)
	router := gin.New()
	h := &Handler{}
	router.PUT("/history", h.ImportUsageHistory)
	router.GET("/history", h.GetUsageHistory)
	for _, body := range []string{`{`, `{"format":"cliproxy-usage-history","version":1,"source":"easycliproxyapi","proxy":[{"hour":1793707200000,"provider":"claude","model":"test","authKind":"oauth","requests":1,"failed":0,"input":10,"output":2,"reasoning":0,"cacheRead":0,"cacheCreation":0,"total":12}],"agents":[]}`} {
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest(http.MethodPut, "/history", strings.NewReader(body)))
		want := http.StatusOK
		if body == "{" {
			want = http.StatusBadRequest
		}
		if w.Code != want {
			t.Fatalf("%d: %s", w.Code, w.Body.String())
		}
	}
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/history", nil))
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"total":12`) || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal(w.Code, w.Body.String())
	}
}
