package redisqueue

import (
	"sync/atomic"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/usagehistory"
)

var historyStore atomic.Pointer[usagehistory.Store]

func SetHistoryStore(store *usagehistory.Store) { historyStore.Store(store) }
func HistoryStore() *usagehistory.Store         { return historyStore.Load() }
