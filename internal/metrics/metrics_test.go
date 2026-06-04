package metrics

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// ─── LabeledCounter Tests ────────────────────────────────────────────────────

func TestLabeledCounter_IncSingle(t *testing.T) {
	lc := NewLabeledCounter()
	lc.Inc(map[string]string{"method": "GET"})
	lc.Inc(map[string]string{"method": "GET"})
	lc.Inc(map[string]string{"method": "POST"})

	snap := lc.Snapshot()
	if len(snap) != 2 {
		t.Fatalf("expected 2 label combos, got %d: %v", len(snap), snap)
	}
	if snap["method=GET"] != 2 {
		t.Errorf("GET count: want 2, got %d", snap["method=GET"])
	}
	if snap["method=POST"] != 1 {
		t.Errorf("POST count: want 1, got %d", snap["method=POST"])
	}
}

func TestLabeledCounter_Add(t *testing.T) {
	lc := NewLabeledCounter()
	lc.Add(5, map[string]string{"status": "2xx"})
	lc.Add(3, map[string]string{"status": "2xx"})

	snap := lc.Snapshot()
	if snap["status=2xx"] != 8 {
		t.Errorf("want 8, got %d", snap["status=2xx"])
	}
}

func TestLabeledCounter_EmptyLabels(t *testing.T) {
	lc := NewLabeledCounter()
	lc.Inc(map[string]string{})

	snap := lc.Snapshot()
	v, ok := snap[""]
	if !ok || v != 1 {
		t.Errorf("empty labels: want key=\"\" val=1, got key=\"\" val=%d", v)
	}
}

func TestLabeledCounter_MultipleDimensions(t *testing.T) {
	lc := NewLabeledCounter()
	lc.Inc(map[string]string{"method": "GET", "status": "2xx"})
	lc.Inc(map[string]string{"method": "GET", "status": "2xx"})
	lc.Inc(map[string]string{"method": "GET", "status": "4xx"})
	lc.Inc(map[string]string{"method": "POST", "status": "2xx"})

	snap := lc.Snapshot()
	if len(snap) != 3 {
		t.Fatalf("expected 3 combos, got %d: %v", len(snap), snap)
	}
	if snap["method=GET,status=2xx"] != 2 {
		t.Errorf("GET+2xx: want 2, got %d", snap["method=GET,status=2xx"])
	}
	if snap["method=GET,status=4xx"] != 1 {
		t.Errorf("GET+4xx: want 1, got %d", snap["method=GET,status=4xx"])
	}
	if snap["method=POST,status=2xx"] != 1 {
		t.Errorf("POST+2xx: want 1, got %d", snap["method=POST,status=2xx"])
	}
}

func TestLabeledCounter_Concurrency(t *testing.T) {
	lc := NewLabeledCounter()
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			lc.Inc(map[string]string{"method": "GET"})
		}()
	}
	wg.Wait()

	snap := lc.Snapshot()
	if snap["method=GET"] != 100 {
		t.Errorf("want 100, got %d", snap["method=GET"])
	}
}

func TestLabeledCounter_SnapshotIsCopy(t *testing.T) {
	lc := NewLabeledCounter()
	lc.Inc(map[string]string{"a": "1"})

	snap := lc.Snapshot()
	snap["a=1"] = 999 // mutate copy

	snap2 := lc.Snapshot()
	if snap2["a=1"] != 1 {
		t.Errorf("mutation leaked: want 1, got %d", snap2["a=1"])
	}
}

// ─── LabeledGauge Tests ──────────────────────────────────────────────────────

func TestLabeledGauge_SetAndSnapshot(t *testing.T) {
	lg := NewLabeledGauge()
	lg.Set(42, map[string]string{"component": "gardener"})
	lg.Set(-5, map[string]string{"component": "evaluator"})

	snap := lg.Snapshot()
	if len(snap) != 2 {
		t.Fatalf("expected 2 combos, got %d", len(snap))
	}
	if snap["component=gardener"] != 42 {
		t.Errorf("gardener: want 42, got %d", snap["component=gardener"])
	}
	if snap["component=evaluator"] != -5 {
		t.Errorf("evaluator: want -5, got %d", snap["component=evaluator"])
	}
}

func TestLabeledGauge_Overwrite(t *testing.T) {
	lg := NewLabeledGauge()
	lg.Set(10, map[string]string{"x": "y"})
	lg.Set(20, map[string]string{"x": "y"})

	snap := lg.Snapshot()
	if snap["x=y"] != 20 {
		t.Errorf("want 20, got %d", snap["x=y"])
	}
}

func TestLabeledGauge_Concurrency(t *testing.T) {
	lg := NewLabeledGauge()
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(val int64) {
			defer wg.Done()
			lg.Set(val, map[string]string{"key": "val"})
		}(int64(i))
	}
	wg.Wait()

	snap := lg.Snapshot()
	if snap["key=val"] < 0 || snap["key=val"] > 49 {
		t.Errorf("unexpected gauge value after concurrent sets: %d", snap["key=val"])
	}
}

// ─── labelKey / parseLabelKey Roundtrip ──────────────────────────────────────

func TestLabelKey_Roundtrip(t *testing.T) {
	tests := []map[string]string{
		{"method": "GET"},
		{"method": "GET", "status": "2xx"},
		{"a": "1", "b": "2", "c": "3"},
		{},
	}
	for _, labels := range tests {
		key := labelKey(labels)
		parsed := parseLabelKey(key)
		if len(parsed) != len(labels) {
			t.Errorf("roundtrip length mismatch: %v → %q → %v", labels, key, parsed)
			continue
		}
		for k, v := range labels {
			if parsed[k] != v {
				t.Errorf("roundtrip value mismatch for %s: want %q, got %q", k, v, parsed[k])
			}
		}
	}
}

func TestLabelKey_Deterministic(t *testing.T) {
	// Different insertion orders should produce the same key
	key1 := labelKey(map[string]string{"b": "2", "a": "1"})
	key2 := labelKey(map[string]string{"a": "1", "b": "2"})
	if key1 != key2 {
		t.Errorf("keys differ: %q vs %q", key1, key2)
	}
	if key1 != "a=1,b=2" {
		t.Errorf("unexpected key: %q", key1)
	}
}

// ─── bucketForStatus Tests ───────────────────────────────────────────────────

func TestBucketForStatus(t *testing.T) {
	tests := []struct {
		code int
		want string
	}{
		{100, "1xx"}, {101, "1xx"}, {199, "1xx"},
		{200, "2xx"}, {201, "2xx"}, {299, "2xx"},
		{301, "3xx"}, {302, "3xx"}, {399, "3xx"},
		{400, "4xx"}, {404, "4xx"}, {499, "4xx"},
		{500, "5xx"}, {502, "5xx"}, {599, "5xx"},
	}
	for _, tt := range tests {
		got := bucketForStatus(tt.code)
		if got != tt.want {
			t.Errorf("bucketForStatus(%d) = %q, want %q", tt.code, got, tt.want)
		}
	}
}

// ─── labelString Tests ───────────────────────────────────────────────────────

func TestLabelString(t *testing.T) {
	tests := []struct {
		labels map[string]string
		want   string
	}{
		{map[string]string{}, ""},
		{map[string]string{"method": "GET"}, `method="GET"`},
		{map[string]string{"status": "2xx", "method": "GET"}, `method="GET",status="2xx"`},
	}
	for _, tt := range tests {
		got := labelString(tt.labels)
		if got != tt.want {
			t.Errorf("labelString(%v) = %q, want %q", tt.labels, got, tt.want)
		}
	}
}

// ─── MetricsMiddleware Tests ─────────────────────────────────────────────────

func TestMetricsMiddleware_RecordsRequest(t *testing.T) {
	handler := MetricsMiddleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))

	req := httptest.NewRequest("GET", "/api/health", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	// Check labeled counters were populated
	methodSnap := httpRequestsByMethod.Snapshot()
	if methodSnap["method=GET"] < 1 {
		t.Errorf("method=GET not recorded: %v", methodSnap)
	}

	statusSnap := httpRequestsByStatus.Snapshot()
	if statusSnap["status=2xx"] < 1 {
		t.Errorf("status=2xx not recorded: %v", statusSnap)
	}

	pathSnap := httpRequestsByPath.Snapshot()
	if pathSnap["path=/api/health"] < 1 {
		t.Errorf("path=/api/health not recorded: %v", pathSnap)
	}
}

func TestMetricsMiddleware_RecordsErrors(t *testing.T) {
	handler := MetricsMiddleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))

	req := httptest.NewRequest("POST", "/api/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	statusSnap := httpRequestsByStatus.Snapshot()
	if statusSnap["status=5xx"] < 1 {
		t.Errorf("status=5xx not recorded: %v", statusSnap)
	}

	methodSnap := httpRequestsByMethod.Snapshot()
	if methodSnap["method=POST"] < 1 {
		t.Errorf("method=POST not recorded: %v", methodSnap)
	}
}

// ─── Prometheus Export Tests ─────────────────────────────────────────────────

func TestPrometheusHandler_ReturnsMetrics(t *testing.T) {
	// Seed some labeled data
	httpRequestsByMethod.Inc(map[string]string{"method": "GET"})
	httpRequestsByStatus.Inc(map[string]string{"status": "2xx"})

	handler := PrometheusHandler()
	req := httptest.NewRequest("GET", "/api/metrics", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	body := rec.Body.String()
	// Verify standard metrics
	if !strings.Contains(body, "bt_http_requests_total") {
		t.Error("missing bt_http_requests_total")
	}
	if !strings.Contains(body, "bt_http_request_duration_ms") {
		t.Error("missing bt_http_request_duration_ms")
	}
	// Verify labeled metrics
	if !strings.Contains(body, "bt_http_requests_by_method") {
		t.Error("missing bt_http_requests_by_method")
	}
	if !strings.Contains(body, "bt_http_requests_by_status") {
		t.Error("missing bt_http_requests_by_status")
	}
	if !strings.Contains(body, "bt_http_requests_by_path") {
		t.Error("missing bt_http_requests_by_path")
	}
	if !strings.Contains(body, `method="GET"`) {
		t.Error("missing method=GET label in output")
	}
	if !strings.Contains(body, `status="2xx"`) {
		t.Error("missing status=2xx label in output")
	}
}

// ─── JSON Export Tests ────────────────────────────────────────────────────────

func TestMetricsJSON_IncludesLabeledMetrics(t *testing.T) {
	httpRequestsByPath.Inc(map[string]string{"path": "/api/test"})

	jsonData := MetricsJSON()

	byPath, ok := jsonData["http_requests_by_path"].([]map[string]interface{})
	if !ok {
		t.Fatal("http_requests_by_path missing or wrong type")
	}
	found := false
	for _, entry := range byPath {
		if entry["path"] == "/api/test" {
			found = true
			if count, ok := entry["count"].(uint64); !ok || count < 1 {
				t.Errorf("count for /api/test: %v", entry["count"])
			}
		}
	}
	if !found {
		t.Errorf("/api/test not found in labeled metrics: %v", byPath)
	}
}

func TestMetricsJSON_Roundtrip(t *testing.T) {
	data := MetricsJSON()
	b, err := json.Marshal(data)
	if err != nil {
		t.Fatalf("failed to marshal MetricsJSON: %v", err)
	}
	var roundtrip map[string]interface{}
	if err := json.Unmarshal(b, &roundtrip); err != nil {
		t.Fatalf("failed to unmarshal MetricsJSON: %v", err)
	}
}

// ─── labeledSnapshotToMap Tests ──────────────────────────────────────────────

func TestLabeledSnapshotToMap(t *testing.T) {
	snap := map[string]uint64{
		"method=GET,status=2xx": 5,
		"method=POST":           3,
	}
	result := labeledSnapshotToMap(snap)
	if len(result) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(result))
	}
	// Find GET+2xx entry
	for _, entry := range result {
		if entry["method"] == "GET" && entry["status"] == "2xx" {
			if entry["count"].(uint64) != 5 {
				t.Errorf("count: want 5, got %v", entry["count"])
			}
		}
	}
}

func TestLabeledSnapshotToMap_Empty(t *testing.T) {
	result := labeledSnapshotToMap(map[string]uint64{})
	if len(result) != 0 {
		t.Errorf("expected empty, got %d entries", len(result))
	}
}

// ─── Health Check Tests ──────────────────────────────────────────────────────

func TestHealthJSON(t *testing.T) {
	b := HealthJSON("v1.0.0")
	var resp HealthResponse
	if err := json.Unmarshal(b, &resp); err != nil {
		t.Fatalf("failed to unmarshal health: %v", err)
	}
	if resp.Status != "ok" {
		t.Errorf("status: want ok, got %s", resp.Status)
	}
	if resp.Version != "v1.0.0" {
		t.Errorf("version: want v1.0.0, got %s", resp.Version)
	}
	if resp.GoVersion == "" {
		t.Error("go_version empty")
	}
	if resp.Uptime == "" {
		t.Error("uptime empty")
	}
}

// ─── Counter / Gauge / Histogram Basic Tests ─────────────────────────────────

func TestCounter(t *testing.T) {
	var c Counter
	c.Inc()
	c.Inc()
	if c.Value() != 2 {
		t.Errorf("want 2, got %d", c.Value())
	}
	c.Add(5)
	if c.Value() != 7 {
		t.Errorf("want 7, got %d", c.Value())
	}
}

func TestGauge(t *testing.T) {
	var g Gauge
	g.Set(10)
	if g.Value() != 10 {
		t.Errorf("want 10, got %d", g.Value())
	}
	g.Inc()
	if g.Value() != 11 {
		t.Errorf("want 11, got %d", g.Value())
	}
	g.Dec()
	if g.Value() != 10 {
		t.Errorf("want 10, got %d", g.Value())
	}
}

func TestHistogram(t *testing.T) {
	h := NewHistogram([]float64{10, 50, 100})
	h.Observe(5)
	h.Observe(25)
	h.Observe(75)
	h.Observe(200)

	h.mu.Lock()
	defer h.mu.Unlock()
	if h.total != 4 {
		t.Errorf("total: want 4, got %d", h.total)
	}
	if h.counts[0] != 1 {
		t.Errorf("bucket <=10: want 1, got %d", h.counts[0])
	}
	if h.counts[1] != 1 {
		t.Errorf("bucket <=50: want 1, got %d", h.counts[1])
	}
	if h.counts[2] != 1 {
		t.Errorf("bucket <=100: want 1, got %d", h.counts[2])
	}
	if h.counts[3] != 1 {
		t.Errorf("bucket +Inf: want 1, got %d", h.counts[3])
	}
}

// ─── RecordTask / GetAgentMetrics Tests ──────────────────────────────────────

func TestRecordTask(t *testing.T) {
	RecordTask("test-agent", true, 150)
	RecordTask("test-agent", false, 300)
	RecordTask("test-agent-2", true, 50)

	stats := GetAgentMetrics()
	if len(stats) < 2 {
		t.Fatalf("expected at least 2 agents, got %d", len(stats))
	}
	// Find test-agent
	for _, s := range stats {
		if s.Name == "test-agent" {
			if s.TotalCount != 2 {
				t.Errorf("total: want 2, got %d", s.TotalCount)
			}
			if s.SuccessCount != 1 {
				t.Errorf("success: want 1, got %d", s.SuccessCount)
			}
			if s.ErrorCount != 1 {
				t.Errorf("errors: want 1, got %d", s.ErrorCount)
			}
			if s.TotalDurationMs != 450 {
				t.Errorf("duration: want 450, got %d", s.TotalDurationMs)
			}
		}
	}
}

// ─── Edge Cases ──────────────────────────────────────────────────────────────

func TestLabeledCounter_NilLabels(t *testing.T) {
	lc := NewLabeledCounter()
	lc.Inc(nil) // should not panic
	snap := lc.Snapshot()
	if _, ok := snap[""]; !ok || snap[""] != 1 {
		t.Errorf("nil labels: want 1, got %v", snap)
	}
}

func TestParseLabelKey_EdgeCases(t *testing.T) {
	// Empty key
	m := parseLabelKey("")
	if len(m) != 0 {
		t.Errorf("empty key: want empty map, got %v", m)
	}

	// Malformed key (no '=')
	m = parseLabelKey("bogus")
	if len(m) != 0 {
		t.Errorf("malformed: want empty map, got %v", m)
	}

	// Key with empty value
	m = parseLabelKey("key=")
	if len(m) != 0 {
		t.Errorf("empty value: want empty map, got %v", m)
	}
}
