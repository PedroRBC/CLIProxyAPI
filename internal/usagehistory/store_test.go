package usagehistory

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

const fixture = `{"format":"cliproxy-usage-history","version":1,"source":"easycliproxyapi","proxy":[{"hour":1793707200000,"provider":"claude","model":"test","authKind":"oauth","requests":2,"failed":0,"input":100,"output":20,"reasoning":5,"cacheRead":40,"cacheCreation":10,"total":120}],"agents":[{"hour":1793707200000,"agent":"codex","model":"test","requests":3,"input":200,"output":20,"reasoning":0,"cacheRead":0,"cacheCreation":0,"total":220}]}`

func TestImportPersistsAndReimportDoesNotDuplicate(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := s.Import([]byte(fixture)); err != nil {
			t.Fatal(err)
		}
	}
	s, err = Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	rows := s.Rows()
	if len(rows) != 2 {
		t.Fatalf("rows=%v", rows)
	}
	for path, mode := range map[string]os.FileMode{dir: 0700, filepath.Join(dir, "imported.json"): 0600, filepath.Join(dir, "live.jsonl"): 0600} {
		info, err := os.Stat(path)
		if err != nil || info.Mode().Perm() != mode {
			t.Fatalf("%s mode mismatch: %v", path, err)
		}
	}
	counts := map[string]int64{}
	for _, row := range rows {
		counts[row.Source] += row.Total
	}
	if counts["easycliproxyapi"] != 120 || counts["agents"] != 220 {
		t.Fatal(counts)
	}
	if err := s.Import([]byte(`{"format":"cliproxy-usage-history","version":1,"source":"easycliproxyapi","proxy":[],"agents":[]}`)); err == nil {
		t.Fatal("accepted a history-losing replacement")
	}
}

func TestQueueJournalSurvivesReopenAndConcurrentWrites(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	payload := []byte(`{"timestamp":"2026-11-03T12:05:00Z","provider":"claude","model":"test","auth_type":"oauth","api_key":"SECRET","client_ip":"PRIVATE","tokens":{"input_tokens":100,"output_tokens":20,"total_tokens":120,"cache_read_tokens":40}}`)
	var wg sync.WaitGroup
	for range 20 {
		wg.Go(func() {
			if err := s.RecordQueue(payload); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	s, err = Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	rows := s.Rows()
	if len(rows) != 1 || rows[0].Requests != 20 || rows[0].Total != 2400 {
		t.Fatal(rows)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "live.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"SECRET", "PRIVATE", "api_key", "client_ip"} {
		if strings.Contains(string(raw), secret) {
			t.Fatalf("persisted %s", secret)
		}
	}
}

func TestCorruptHistoryIsNotSilentlyReplaced(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "live.jsonl"), []byte(`{"incomplete":`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(dir); err == nil {
		t.Fatal("ignored corrupt history")
	}
}

func TestMissingJournalNewlineCannotCorruptNextAppend(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "live.jsonl"), []byte(`{"source":"live"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(dir); err == nil || !strings.Contains(err.Error(), "incomplete") {
		t.Fatal(err)
	}
}
