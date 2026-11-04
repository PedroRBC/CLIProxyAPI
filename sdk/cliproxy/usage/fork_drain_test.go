package usage

import (
	"context"
	"testing"
)

type blockingUsagePlugin struct {
	release chan struct{}
	got     chan Record
}

func (p *blockingUsagePlugin) HandleUsage(_ context.Context, record Record) {
	<-p.release
	p.got <- record
}

func TestManagerWaitDrainedWaitsForQueuedRecords(t *testing.T) {
	if !NewManager(0).WaitDrained(context.Background()) {
		t.Fatal("a manager that never started must report drained")
	}

	m := NewManager(0)
	plugin := &blockingUsagePlugin{release: make(chan struct{}), got: make(chan Record, 2)}
	m.Register(plugin)
	m.Start(context.Background())
	m.Publish(context.Background(), Record{RequestID: "a"})
	m.Publish(context.Background(), Record{RequestID: "b"})
	m.Stop()

	expired, cancel := context.WithCancel(context.Background())
	cancel()
	if m.WaitDrained(expired) {
		t.Fatal("WaitDrained reported drained while a plugin is still blocked")
	}

	close(plugin.release)
	if !m.WaitDrained(context.Background()) {
		t.Fatal("WaitDrained did not report drained")
	}
	if len(plugin.got) != 2 {
		t.Fatalf("delivered %d records before drain finished, want 2", len(plugin.got))
	}
}
