package redisqueue

import (
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/usagehistory"
)

func TestHistoryPersistsBeforeDestructiveSubscriberDelivery(t *testing.T) {
	oldEnabled, oldStore := Enabled(), HistoryStore()
	t.Cleanup(func() { SetHistoryStore(oldStore); SetEnabled(oldEnabled) })
	store, err := usagehistory.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	SetHistoryStore(store)
	SetEnabled(true)
	sub, cancel := SubscribeUsage()
	defer cancel()
	<-sub
	Enqueue([]byte(`{"timestamp":"2026-11-03T12:05:00Z","provider":"claude","model":"test","tokens":{"input_tokens":10,"output_tokens":2}}`))
	<-sub
	if len(store.Rows()) != 1 || store.Rows()[0].Total != 12 {
		t.Fatal(store.Rows())
	}
	if records := PopOldest(10); len(records) != 0 {
		t.Fatal("subscriber delivery should not be queued twice")
	}
}
