package management

import (
	"io"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/redisqueue"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/usagehistory"
)

func (h *Handler) GetUsageHistory(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	store := redisqueue.HistoryStore()
	if store == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "persistent history unavailable"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"rows": store.Rows()})
}

func (h *Handler) ImportUsageHistory(c *gin.Context) {
	store := redisqueue.HistoryStore()
	if store == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "persistent history unavailable"})
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, usagehistory.MaxImportBytes))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid or oversized history import"})
		return
	}
	if err := store.Import(body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}
