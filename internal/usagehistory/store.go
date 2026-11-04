// Package usagehistory persists sanitized hourly usage independently of queue consumers.
package usagehistory

import (
	"bufio"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/MoeForge/moego/ojson"
)

const HourMS int64 = 3600000
const MaxImportBytes = 50 << 20

type Row struct {
	ID            string `json:"id"`
	Source        string `json:"source"`
	Hour          int64  `json:"hour"`
	Provider      string `json:"provider"`
	Model         string `json:"model"`
	AuthKind      string `json:"authKind"`
	Requests      int64  `json:"requests"`
	Failed        int64  `json:"failed"`
	Input         int64  `json:"input"`
	Output        int64  `json:"output"`
	Reasoning     int64  `json:"reasoning"`
	CacheRead     int64  `json:"cacheRead"`
	CacheCreation int64  `json:"cacheCreation"`
	Total         int64  `json:"total"`
}

func (r Row) counts() []int64 {
	return []int64{r.Requests, r.Failed, r.Input, r.Output, r.Reasoning, r.CacheRead, r.CacheCreation, r.Total}
}

func (r *Row) add(v Row) {
	r.Requests += v.Requests
	r.Failed += v.Failed
	r.Input += v.Input
	r.Output += v.Output
	r.Reasoning += v.Reasoning
	r.CacheRead += v.CacheRead
	r.CacheCreation += v.CacheCreation
	r.Total += v.Total
}

func (r *Row) identify() {
	r.ID = fmt.Sprintf("%s|%d|%s|%s|%s", r.Source, r.Hour, r.Provider, r.Model, r.AuthKind)
}

type Store struct {
	mu       sync.Mutex
	dir      string
	imported map[string]Row
	live     map[string]Row
}

func Open(dir string) (*Store, error) {
	s := &Store{dir: dir, imported: map[string]Row{}, live: map[string]Row{}}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	data, err := os.ReadFile(filepath.Join(dir, "imported.json"))
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	if err == nil {
		if !ojson.ValidBytes(data) || !ojson.ParseBytes(data).IsArray() {
			return nil, errors.New("invalid imported history")
		}
		for _, item := range ojson.ParseBytes(data).Array() {
			row, errRow := parseRow(item, item.Get("source").String(), false)
			if errRow != nil {
				return nil, errRow
			}
			s.imported[row.ID] = row
		}
	}
	f, err := os.Open(filepath.Join(dir, "live.jsonl"))
	if os.IsNotExist(err) {
		return s, atomicWrite(filepath.Join(dir, "live.jsonl"), nil)
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if info.Size() > 0 {
		last := make([]byte, 1)
		if _, err := f.ReadAt(last, info.Size()-1); err != nil {
			return nil, err
		}
		if last[0] != '\n' {
			return nil, errors.New("incomplete live usage journal; original preserved")
		}
	}
	scan := bufio.NewScanner(f)
	scan.Buffer(make([]byte, 4096), 1<<20)
	for scan.Scan() {
		if !ojson.ValidBytes(scan.Bytes()) {
			return nil, errors.New("invalid live usage journal; original preserved")
		}
		row, errRow := parseRow(ojson.ParseBytes(scan.Bytes()), "live", false)
		if errRow != nil {
			return nil, errRow
		}
		previous := s.live[row.ID]
		if previous.ID == "" {
			previous = row
		} else {
			previous.add(row)
		}
		s.live[row.ID] = previous
	}
	if err := scan.Err(); err != nil {
		return nil, err
	}
	return s, nil
}

func encode(value any) ([]byte, error) {
	doc := ojson.NewDocString("{}")
	if err := doc.Set("value", value); err != nil {
		return nil, err
	}
	return []byte(ojson.GetBytes(doc.Bytes(), "value").Raw), nil
}

func parseRow(item ojson.Result, source string, agentImport bool) (Row, error) {
	if !item.IsObject() || (source != "live" && source != "easycliproxyapi" && source != "agents") {
		return Row{}, errors.New("invalid usage source")
	}
	r := Row{Source: source, Hour: item.Get("hour").Int(), Provider: item.Get("provider").String(), Model: item.Get("model").String(), AuthKind: item.Get("authKind").String()}
	if agentImport {
		r.Provider = item.Get("agent").String()
	}
	if r.Provider == "" {
		r.Provider = "unknown"
	}
	if r.Model == "" {
		r.Model = "unknown"
	}
	if r.AuthKind == "" && source == "agents" {
		r.AuthKind = "oauth"
	}
	if len(r.Provider) > 160 || len(r.Model) > 160 || strings.ContainsAny(r.Provider+r.Model, "|\r\n") || (r.AuthKind != "oauth" && r.AuthKind != "apikey") {
		return Row{}, errors.New("invalid usage dimensions")
	}
	hour := item.Get("hour")
	if hour.Type != ojson.Number || hour.Float() != float64(r.Hour) || r.Hour < 946684800000 || r.Hour%HourMS != 0 || r.Hour > time.Now().UnixMilli()+HourMS {
		return Row{}, errors.New("invalid usage hour")
	}
	for key, target := range map[string]*int64{"requests": &r.Requests, "failed": &r.Failed, "input": &r.Input, "output": &r.Output, "reasoning": &r.Reasoning, "cacheRead": &r.CacheRead, "cacheCreation": &r.CacheCreation, "total": &r.Total} {
		v := item.Get(key)
		if key == "failed" && !v.Exists() {
			continue
		}
		if v.Type != ojson.Number || v.Float() < 0 || v.Float() > 1e15 || math.Trunc(v.Float()) != v.Float() {
			return Row{}, fmt.Errorf("invalid usage count %s", key)
		}
		*target = v.Int()
	}
	if r.Input+r.Output != r.Total || r.Failed > r.Requests {
		return Row{}, errors.New("inconsistent usage counts")
	}
	r.identify()
	return r, nil
}

// Import replaces the local export, never adds it to itself or discards newer rows.
func (s *Store) Import(data []byte) error {
	if len(data) > MaxImportBytes || !ojson.ValidBytes(data) {
		return errors.New("invalid or oversized history import")
	}
	doc := ojson.ParseBytes(data)
	if doc.Get("format").String() != "cliproxy-usage-history" || doc.Get("version").Int() != 1 || doc.Get("source").String() != "easycliproxyapi" {
		return errors.New("expected EasyCLIProxyAPI history v1")
	}
	next := map[string]Row{}
	count := 0
	for _, group := range []string{"proxy", "agents"} {
		items := doc.Get(group)
		if !items.IsArray() {
			return errors.New("missing history arrays")
		}
		for _, item := range items.Array() {
			count++
			if count > 100000 {
				return errors.New("too many history rows")
			}
			source := "easycliproxyapi"
			if group == "agents" {
				source = "agents"
			}
			row, err := parseRow(item, source, group == "agents")
			if err != nil {
				return err
			}
			if previous, ok := next[row.ID]; ok {
				row.add(previous)
			}
			for _, n := range row.counts() {
				if n > 1e15 {
					return errors.New("aggregated usage count too large")
				}
			}
			next[row.ID] = row
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, previous := range s.imported {
		row, ok := next[id]
		if !ok {
			return errors.New("import would remove existing history")
		}
		for i, n := range previous.counts() {
			if row.counts()[i] < n {
				return errors.New("import would reduce existing history")
			}
		}
	}
	rows := make([]Row, 0, len(next))
	for _, row := range next {
		rows = append(rows, row)
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].ID < rows[j].ID })
	encoded, err := encode(rows)
	if err != nil {
		return err
	}
	if err := atomicWrite(filepath.Join(s.dir, "imported.json"), encoded); err != nil {
		return err
	}
	s.imported = next
	return nil
}

func atomicWrite(path string, data []byte) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".history-")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Rename(f.Name(), path); err != nil {
		return err
	}
	dir, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}

// RecordQueue runs before destructive queue delivery. Only aggregate fields reach disk.
func (s *Store) RecordQueue(data []byte) error {
	if !ojson.ValidBytes(data) {
		return errors.New("invalid queued usage")
	}
	doc := ojson.ParseBytes(data)
	stamp, err := time.Parse(time.RFC3339Nano, doc.Get("timestamp").String())
	if err != nil {
		return err
	}
	r := Row{Source: "live", Hour: stamp.UnixMilli() / HourMS * HourMS, Provider: doc.Get("provider").String(), Model: doc.Get("model").String(), AuthKind: "oauth", Requests: 1}
	if r.Provider == "" {
		r.Provider = "unknown"
	}
	if r.Model == "" {
		r.Model = "unknown"
	}
	if doc.Get("auth_type").String() == "apikey" {
		r.AuthKind = "apikey"
	}
	if doc.Get("failed").Bool() {
		r.Failed = 1
	}
	tokens := doc.Get("tokens")
	r.Input = tokens.Get("input_tokens").Int()
	r.Output = tokens.Get("output_tokens").Int()
	r.Reasoning = tokens.Get("reasoning_tokens").Int()
	r.CacheRead = tokens.Get("cache_read_tokens").Int()
	r.CacheCreation = tokens.Get("cache_creation_tokens").Int()
	r.Total = r.Input + r.Output
	r.identify()
	encoded, err := encode(r)
	if err != nil {
		return err
	}
	if _, err := parseRow(ojson.ParseBytes(encoded), "live", false); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := os.OpenFile(filepath.Join(s.dir, "live.jsonl"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return err
	}
	if _, err = f.Write(append(encoded, '\n')); err == nil {
		err = f.Sync()
	}
	if err != nil {
		return errors.Join(err, f.Truncate(info.Size()), f.Sync())
	}
	previous, ok := s.live[r.ID]
	if ok {
		previous.add(r)
		r = previous
	}
	s.live[r.ID] = r
	return nil
}

func (s *Store) Rows() []Row {
	s.mu.Lock()
	defer s.mu.Unlock()
	rows := make([]Row, 0, len(s.imported)+len(s.live))
	for _, row := range s.imported {
		rows = append(rows, row)
	}
	for _, row := range s.live {
		rows = append(rows, row)
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].ID < rows[j].ID })
	return rows
}
