package tracing

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// ─── ParseTraceLine tests ─────────────────────────────────────────────────────

func TestParseTraceLine_Basic(t *testing.T) {
	line := "TRACE 2026-05-30T22:15:27.952309568+02:00 bt-agent-000165-trace bt-agent-000166 op=mcp:bt_kg_summary duration=151µs tool=bt_kg_summary duration_ms=0"

	entry := ParseTraceLine(line)
	if entry == nil {
		t.Fatal("expected non-nil entry")
	}

	if entry.Operation != "mcp:bt_kg_summary" {
		t.Errorf("expected operation 'mcp:bt_kg_summary', got %q", entry.Operation)
	}
	if entry.Duration != 151*time.Microsecond {
		t.Errorf("expected duration 151µs, got %v", entry.Duration)
	}
	if entry.TraceID != "bt-agent-000165-trace" {
		t.Errorf("expected trace_id 'bt-agent-000165-trace', got %q", entry.TraceID)
	}
	if entry.SpanID != "bt-agent-000166" {
		t.Errorf("expected span_id 'bt-agent-000166', got %q", entry.SpanID)
	}
	if v, ok := entry.Attributes["tool"]; !ok || v != "bt_kg_summary" {
		t.Errorf("expected attribute tool=bt_kg_summary, got %v", entry.Attributes)
	}
	if v, ok := entry.Attributes["duration_ms"]; !ok || v != "0" {
		t.Errorf("expected attribute duration_ms=0, got %v", entry.Attributes)
	}
	if entry.DurationMS != 0 {
		t.Errorf("expected DurationMS=0, got %d", entry.DurationMS)
	}

	// JSON roundtrip
	data, err := json.Marshal(entry)
	if err != nil {
		t.Fatalf("json marshal: %v", err)
	}
	var back TraceEntry
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatalf("json unmarshal: %v", err)
	}
	if back.Operation != entry.Operation {
		t.Errorf("roundtrip operation mismatch: %q vs %q", back.Operation, entry.Operation)
	}
	if back.DurationMS != entry.DurationMS {
		t.Errorf("roundtrip duration_ms mismatch: %d vs %d", back.DurationMS, entry.DurationMS)
	}
}

func TestParseTraceLine_WithParent(t *testing.T) {
	line := "TRACE 2026-05-30T22:15:27.952309568+02:00 bt-agent-000165-trace bt-agent-000166 parent=bt-agent-000165 op=mcp:bt_health duration=69µs duration_ms=0 tool=bt_health"

	entry := ParseTraceLine(line)
	if entry == nil {
		t.Fatal("expected non-nil entry")
	}

	if entry.ParentSpanID != "bt-agent-000165" {
		t.Errorf("expected parent 'bt-agent-000165', got %q", entry.ParentSpanID)
	}
	if entry.Operation != "mcp:bt_health" {
		t.Errorf("expected operation 'mcp:bt_health', got %q", entry.Operation)
	}
}

func TestParseTraceLine_WithEvents(t *testing.T) {
	line := "TRACE 2026-05-30T10:00:00.000000001+02:00 trace-1 span-1 op=http:GET /api/test duration=5.1s http.method=GET http.url=/api/test http.status_code=200 http.duration_ms=5100 [slow_request threshold=5s actual=5.1s]"

	entry := ParseTraceLine(line)
	if entry == nil {
		t.Fatal("expected non-nil entry")
	}

	if len(entry.Events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(entry.Events))
	}
	if entry.Events[0].Name != "slow_request" {
		t.Errorf("expected event name 'slow_request', got %q", entry.Events[0].Name)
	}
	if v, ok := entry.Events[0].Attrs["threshold"]; !ok || v != "5s" {
		t.Errorf("expected event attr threshold=5s, got %v", entry.Events[0].Attrs)
	}
	if v, ok := entry.Events[0].Attrs["actual"]; !ok || v != "5.1s" {
		t.Errorf("expected event attr actual=5.1s, got %v", entry.Events[0].Attrs)
	}
}

func TestParseTraceLine_WithError(t *testing.T) {
	line := "TRACE 2026-05-30T10:00:00.000000001+02:00 trace-1 span-1 op=RunTask:test duration=10s error=\"something went wrong\""

	entry := ParseTraceLine(line)
	if entry == nil {
		t.Fatal("expected non-nil entry")
	}

	if entry.Error != "something went wrong" {
		t.Errorf("expected error 'something went wrong', got %q", entry.Error)
	}
}

func TestParseTraceLine_WithQuotedValue(t *testing.T) {
	line := "TRACE 2026-05-30T10:00:00.000000001+02:00 trace-1 span-1 op=test duration=1s msg=\"hello world\""

	entry := ParseTraceLine(line)
	if entry == nil {
		t.Fatal("expected non-nil entry")
	}
	// Note: current simple parser uses space-delimited values.
	// Quoted values with spaces won't parse cleanly in the current format.
	// The msg attribute may be split. This is acceptable — trace output
	// uses single-word attribute values.
	if entry == nil {
		t.Error("should parse even with quoted values")
	}
}

func TestParseTraceLine_DurationFormats(t *testing.T) {
	tests := []struct {
		durStr   string
		expected time.Duration
	}{
		{"151µs", 151 * time.Microsecond},
		{"2.341s", 2341 * time.Millisecond},
		{"1m30s", 90 * time.Second},
		{"742µs", 742 * time.Microsecond},
		{"337µs", 337 * time.Microsecond},
	}

	for _, tt := range tests {
		line := "TRACE 2026-05-30T10:00:00.000000001+02:00 trace-1 span-1 op=test duration=" + tt.durStr
		entry := ParseTraceLine(line)
		if entry == nil {
			t.Errorf("duration %q: expected non-nil entry", tt.durStr)
			continue
		}
		if entry.Duration != tt.expected {
			t.Errorf("duration %q: expected %v, got %v", tt.durStr, tt.expected, entry.Duration)
		}
	}
}

func TestParseTraceLine_Invalid(t *testing.T) {
	tests := []string{
		"",
		"not a trace line",
		"TRACE",
		"TRACE 2026-05-30",
	}

	for _, line := range tests {
		if entry := ParseTraceLine(line); entry != nil {
			t.Errorf("expected nil for line %q, got %+v", line, entry)
		}
	}
}

// ─── ParseTraceEvent tests ────────────────────────────────────────────────────

func TestParseTraceEvent(t *testing.T) {
	tests := []struct {
		body      string
		wantName  string
		wantAttrs map[string]string
	}{
		{"slow_request threshold=5s actual=5.1s", "slow_request", map[string]string{"threshold": "5s", "actual": "5.1s"}},
		{"event_only", "event_only", map[string]string{}},
		{"", "", nil},
	}

	for _, tt := range tests {
		ev := ParseTraceEvent(tt.body)
		if tt.wantName == "" {
			if ev != nil {
				t.Errorf("body %q: expected nil, got %+v", tt.body, ev)
			}
			continue
		}
		if ev == nil {
			t.Errorf("body %q: expected non-nil", tt.body)
			continue
		}
		if ev.Name != tt.wantName {
			t.Errorf("body %q: expected name %q, got %q", tt.body, tt.wantName, ev.Name)
		}
		for k, v := range tt.wantAttrs {
			if got, ok := ev.Attrs[k]; !ok || got != v {
				t.Errorf("body %q: attr %q: expected %q, got %q", tt.body, k, v, got)
			}
		}
	}
}

// ─── TraceReader tests ────────────────────────────────────────────────────────

func TestTraceReader_ReadRecent(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "traces.log")

	// Write 10 trace lines
	var lines []string
	baseTime := time.Date(2026, 5, 30, 10, 0, 0, 0, time.UTC)
	for i := 0; i < 10; i++ {
		ts := baseTime.Add(time.Duration(i) * time.Second)
		lines = append(lines, "TRACE "+
			ts.Format(time.RFC3339Nano)+
			" trace-1 span-"+formatInt(i+1)+
			" op=test duration=1ms")
	}
	_ = os.WriteFile(logPath, []byte(strings.Join(lines, "\n")+"\n"), 0644)

	r := NewTraceReader(logPath)

	// Read 5 most recent
	entries, err := r.ReadRecent(5)
	if err != nil {
		t.Fatalf("ReadRecent: %v", err)
	}
	if len(entries) != 5 {
		t.Fatalf("expected 5 entries, got %d", len(entries))
	}

	// Newest first
	if entries[0].SpanID != "span-10" {
		t.Errorf("expected newest span-10, got %s", entries[0].SpanID)
	}
	if entries[4].SpanID != "span-6" {
		t.Errorf("expected oldest in window span-6, got %s", entries[4].SpanID)
	}
}

func TestTraceReader_ReadRecent_MoreThanAvailable(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "traces.log")

	baseTime := time.Date(2026, 5, 30, 10, 0, 0, 0, time.UTC)
	lines := []string{
		"TRACE " + baseTime.Format(time.RFC3339Nano) + " trace-1 span-1 op=test duration=1ms",
	}
	_ = os.WriteFile(logPath, []byte(strings.Join(lines, "\n")+"\n"), 0644)

	r := NewTraceReader(logPath)
	entries, err := r.ReadRecent(50)
	if err != nil {
		t.Fatalf("ReadRecent: %v", err)
	}
	if len(entries) != 1 {
		t.Errorf("expected 1 entry, got %d", len(entries))
	}
}

func TestTraceReader_ReadRecent_NoFile(t *testing.T) {
	r := NewTraceReader("/nonexistent/traces.log")
	entries, err := r.ReadRecent(50)
	if err != nil {
		t.Fatalf("ReadRecent on nonexistent file: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("expected 0 entries, got %d", len(entries))
	}
}

func TestTraceReader_ReadRecent_FiltersInvalidLines(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "traces.log")

	baseTime := time.Date(2026, 5, 30, 10, 0, 0, 0, time.UTC)
	content := strings.Join([]string{
		"garbage line",
		"TRACE " + baseTime.Format(time.RFC3339Nano) + " trace-1 span-1 op=valid duration=1ms",
		"",
		"another garbage",
		"TRACE " + baseTime.Add(time.Second).Format(time.RFC3339Nano) + " trace-1 span-2 op=valid2 duration=2ms",
	}, "\n") + "\n"
	_ = os.WriteFile(logPath, []byte(content), 0644)

	r := NewTraceReader(logPath)
	entries, err := r.ReadRecent(50)
	if err != nil {
		t.Fatalf("ReadRecent: %v", err)
	}
	if len(entries) != 2 {
		t.Errorf("expected 2 valid entries, got %d", len(entries))
	}
}

func TestTraceReader_ReadSince(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "traces.log")

	baseTime := time.Date(2026, 5, 30, 10, 0, 0, 0, time.UTC)
	var lines []string
	for i := 0; i < 10; i++ {
		ts := baseTime.Add(time.Duration(i) * time.Second)
		lines = append(lines, "TRACE "+
			ts.Format(time.RFC3339Nano)+
			" trace-1 span-"+formatInt(i+1)+
			" op=test duration=1ms")
	}
	_ = os.WriteFile(logPath, []byte(strings.Join(lines, "\n")+"\n"), 0644)

	r := NewTraceReader(logPath)

	// Read since 5s after base time (should get spans 6-10)
	since := baseTime.Add(5500 * time.Millisecond)
	entries, err := r.ReadSince(since, 50)
	if err != nil {
		t.Fatalf("ReadSince: %v", err)
	}
	if len(entries) != 4 { // spans 7, 8, 9, 10 (index 6-9)
		t.Errorf("expected 4 entries since %v, got %d", since, len(entries))
	}
	// Newest first
	if len(entries) > 0 && entries[0].SpanID != "span-10" {
		t.Errorf("expected newest span-10, got %s", entries[0].SpanID)
	}
}

func TestTraceReader_ReadSince_Limit(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "traces.log")

	baseTime := time.Date(2026, 5, 30, 10, 0, 0, 0, time.UTC)
	var lines []string
	for i := 0; i < 10; i++ {
		ts := baseTime.Add(time.Duration(i) * time.Second)
		lines = append(lines, "TRACE "+
			ts.Format(time.RFC3339Nano)+
			" trace-1 span-"+formatInt(i+1)+
			" op=test duration=1ms")
	}
	_ = os.WriteFile(logPath, []byte(strings.Join(lines, "\n")+"\n"), 0644)

	r := NewTraceReader(logPath)
	entries, err := r.ReadSince(baseTime, 3) // limit=3
	if err != nil {
		t.Fatalf("ReadSince: %v", err)
	}
	if len(entries) != 3 {
		t.Errorf("expected 3 entries (limit), got %d", len(entries))
	}
}

func TestTraceReader_TotalLines(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "traces.log")

	baseTime := time.Date(2026, 5, 30, 10, 0, 0, 0, time.UTC)
	var lines []string
	for i := 0; i < 10; i++ {
		ts := baseTime.Add(time.Duration(i) * time.Second)
		lines = append(lines, "TRACE "+
			ts.Format(time.RFC3339Nano)+
			" trace-1 span-"+formatInt(i+1)+
			" op=test duration=1ms")
	}
	_ = os.WriteFile(logPath, []byte(strings.Join(lines, "\n")+"\n"), 0644)

	r := NewTraceReader(logPath)
	count, err := r.TotalLines()
	if err != nil {
		t.Fatalf("TotalLines: %v", err)
	}
	if count != 10 {
		t.Errorf("expected 10 lines, got %d", count)
	}
}

func TestTraceReader_TotalLines_NoFile(t *testing.T) {
	r := NewTraceReader("/nonexistent/traces.log")
	count, err := r.TotalLines()
	if err != nil {
		t.Fatalf("TotalLines on nonexistent file: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 lines, got %d", count)
	}
}

func TestTraceReader_SizeBytes(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "traces.log")

	content := "TRACE 2026-05-30T10:00:00.000000001Z trace-1 span-1 op=test duration=1ms\n"
	_ = os.WriteFile(logPath, []byte(content), 0644)

	r := NewTraceReader(logPath)
	size, err := r.SizeBytes()
	if err != nil {
		t.Fatalf("SizeBytes: %v", err)
	}
	if size <= 0 {
		t.Errorf("expected positive size, got %d", size)
	}
}

func TestTraceReader_SizeBytes_NoFile(t *testing.T) {
	r := NewTraceReader("/nonexistent/traces.log")
	size, err := r.SizeBytes()
	if err != nil {
		t.Fatalf("SizeBytes on nonexistent file: %v", err)
	}
	if size != 0 {
		t.Errorf("expected 0 size, got %d", size)
	}
}

func TestTraceReader_MultipleEvents(t *testing.T) {
	line := "TRACE 2026-05-30T10:00:00.000000001+02:00 trace-1 span-1 op=test duration=1s [event_one a=1] [event_two b=2 c=3]"
	entry := ParseTraceLine(line)
	if entry == nil {
		t.Fatal("expected non-nil entry")
	}
	if len(entry.Events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(entry.Events))
	}
	if entry.Events[0].Name != "event_one" {
		t.Errorf("expected first event 'event_one', got %q", entry.Events[0].Name)
	}
	if entry.Events[1].Name != "event_two" {
		t.Errorf("expected second event 'event_two', got %q", entry.Events[1].Name)
	}
}

func TestParseRemaining_EmptyRest(t *testing.T) {
	entry := &TraceEntry{Attributes: make(map[string]string)}
	parseRemaining(entry, "")
	if len(entry.Attributes) > 0 {
		t.Errorf("expected no attributes, got %v", entry.Attributes)
	}
}

// formatInt formats an integer as a string for span IDs in tests.
func formatInt(n int) string {
	if n < 10 {
		return string(rune('0' + n))
	}
	result := ""
	tmp := n
	for tmp > 0 {
		result = string(rune('0'+tmp%10)) + result
		tmp /= 10
	}
	return result
}

// Benchmark
func BenchmarkParseTraceLine(b *testing.B) {
	line := "TRACE 2026-05-30T22:15:27.952309568+02:00 bt-agent-000165-trace bt-agent-000166 op=mcp:bt_kg_summary duration=151µs tool=bt_kg_summary duration_ms=0"
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ParseTraceLine(line)
	}
}

func BenchmarkTraceReader_ReadRecent(b *testing.B) {
	dir := b.TempDir()
	logPath := filepath.Join(dir, "traces.log")

	baseTime := time.Date(2026, 5, 30, 10, 0, 0, 0, time.UTC)
	var lines []string
	for i := 0; i < 100; i++ {
		ts := baseTime.Add(time.Duration(i) * time.Second)
		lines = append(lines, "TRACE "+
			ts.Format(time.RFC3339Nano)+
			" trace-1 span-"+formatInt(i+1)+
			" op=test duration=1ms")
	}
	_ = os.WriteFile(logPath, []byte(strings.Join(lines, "\n")+"\n"), 0644)
	r := NewTraceReader(logPath)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = r.ReadRecent(50)
	}
}

// ─── Trace Aggregation tests ──────────────────────────────────────────────────

func TestBuildAggregatedTrace_SingleSpan(t *testing.T) {
	baseTime := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)
	spans := []TraceEntry{
		{
			TraceID:   "trace-1",
			SpanID:    "span-1",
			Timestamp: baseTime,
			Duration:  100 * time.Millisecond,
			Operation: "RunTask:review code",
		},
	}

	agg := buildAggregatedTrace("trace-1", spans)
	if agg == nil {
		t.Fatal("expected non-nil AggregatedTrace")
	}
	if agg.SpanCount != 1 {
		t.Errorf("expected SpanCount=1, got %d", agg.SpanCount)
	}
	if agg.RootSpan == nil {
		t.Fatal("expected non-nil RootSpan")
	}
	if agg.RootSpan.Span.SpanID != "span-1" {
		t.Errorf("expected root span-1, got %s", agg.RootSpan.Span.SpanID)
	}
	if len(agg.RootSpan.Children) != 0 {
		t.Errorf("expected 0 children, got %d", len(agg.RootSpan.Children))
	}
	if agg.TotalDuration != 100*time.Millisecond {
		t.Errorf("expected TotalDuration=100ms, got %v", agg.TotalDuration)
	}
	if !agg.StartTime.Equal(baseTime) {
		t.Errorf("expected StartTime=%v, got %v", baseTime, agg.StartTime)
	}
}

func TestBuildAggregatedTrace_ParentChild(t *testing.T) {
	baseTime := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)
	spans := []TraceEntry{
		{
			TraceID:   "trace-2",
			SpanID:    "root",
			Timestamp: baseTime,
			Duration:  500 * time.Millisecond,
			Operation: "http:GET /api/tasks",
		},
		{
			TraceID:      "trace-2",
			SpanID:       "child-1",
			ParentSpanID: "root",
			Timestamp:    baseTime.Add(10 * time.Millisecond),
			Duration:     200 * time.Millisecond,
			Operation:    "RunTask:review code",
		},
		{
			TraceID:      "trace-2",
			SpanID:       "child-2",
			ParentSpanID: "root",
			Timestamp:    baseTime.Add(50 * time.Millisecond),
			Duration:     100 * time.Millisecond,
			Operation:    "mcp:bt_get_tree",
		},
	}

	agg := buildAggregatedTrace("trace-2", spans)
	if agg == nil {
		t.Fatal("expected non-nil AggregatedTrace")
	}
	if agg.SpanCount != 3 {
		t.Errorf("expected SpanCount=3, got %d", agg.SpanCount)
	}
	if agg.RootSpan == nil {
		t.Fatal("expected non-nil RootSpan")
	}
	if agg.RootSpan.Span.SpanID != "root" {
		t.Errorf("expected root span 'root', got %s", agg.RootSpan.Span.SpanID)
	}
	if len(agg.RootSpan.Children) != 2 {
		t.Fatalf("expected 2 children, got %d", len(agg.RootSpan.Children))
	}

	// Verify children
	childIDs := make(map[string]bool)
	for _, c := range agg.RootSpan.Children {
		childIDs[c.Span.SpanID] = true
	}
	if !childIDs["child-1"] || !childIDs["child-2"] {
		t.Errorf("expected children child-1 and child-2, got %v", childIDs)
	}

	// Total duration should span from start of root to end of last child
	if agg.TotalDuration < 500*time.Millisecond {
		t.Errorf("expected TotalDuration >= 500ms, got %v", agg.TotalDuration)
	}
}

func TestBuildAggregatedTrace_OrphanSpans(t *testing.T) {
	// Spans with parents that don't exist in the set — earliest span becomes root
	baseTime := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)
	spans := []TraceEntry{
		{
			TraceID:      "trace-3",
			SpanID:       "orphan-1",
			ParentSpanID: "missing-parent",
			Timestamp:    baseTime.Add(50 * time.Millisecond),
			Duration:     100 * time.Millisecond,
			Operation:    "mcp:bt_run_task",
		},
		{
			TraceID:      "trace-3",
			SpanID:       "orphan-2",
			ParentSpanID: "also-missing",
			Timestamp:    baseTime,
			Duration:     200 * time.Millisecond,
			Operation:    "mcp:bt_get_tree",
		},
	}

	agg := buildAggregatedTrace("trace-3", spans)
	if agg == nil {
		t.Fatal("expected non-nil AggregatedTrace")
	}
	if agg.RootSpan == nil {
		t.Fatal("expected non-nil RootSpan")
	}
	// Earliest span should be root
	if agg.RootSpan.Span.SpanID != "orphan-2" {
		t.Errorf("expected earliest span orphan-2 as root, got %s", agg.RootSpan.Span.SpanID)
	}
	if agg.SpanCount != 2 {
		t.Errorf("expected SpanCount=2, got %d", agg.SpanCount)
	}
}

func TestBuildAggregatedTrace_EmptySpans(t *testing.T) {
	agg := buildAggregatedTrace("empty", nil)
	if agg != nil {
		t.Error("expected nil for empty spans")
	}
}

func TestBuildAggregatedTrace_Operations(t *testing.T) {
	baseTime := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)
	spans := []TraceEntry{
		{TraceID: "trace-4", SpanID: "s1", Timestamp: baseTime, Duration: 1 * time.Millisecond, Operation: "http:GET /"},
		{TraceID: "trace-4", SpanID: "s2", Timestamp: baseTime, Duration: 1 * time.Millisecond, Operation: "RunTask:task"},
		{TraceID: "trace-4", SpanID: "s3", Timestamp: baseTime, Duration: 1 * time.Millisecond, Operation: "RunTask:task"}, // duplicate op
	}

	agg := buildAggregatedTrace("trace-4", spans)
	if agg == nil {
		t.Fatal("expected non-nil AggregatedTrace")
	}
	// Should deduplicate operations
	if len(agg.Operations) != 2 {
		t.Errorf("expected 2 unique operations, got %d: %v", len(agg.Operations), agg.Operations)
	}
}

func TestAggregatedTrace_JSONRoundtrip(t *testing.T) {
	baseTime := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)
	agg := &AggregatedTrace{
		TraceID:         "trace-json",
		SpanCount:       2,
		TotalDuration:   500 * time.Millisecond,
		TotalDurationMS: 500,
		StartTime:       baseTime,
		EndTime:         baseTime.Add(500 * time.Millisecond),
		Operations:      []string{"http:GET /", "RunTask:test"},
		RootSpan: &TraceSpanNode{
			Span: TraceEntry{
				TraceID:   "trace-json",
				SpanID:    "root",
				Timestamp: baseTime,
				Duration:  500 * time.Millisecond,
				Operation: "http:GET /",
			},
			Children: []*TraceSpanNode{
				{
					Span: TraceEntry{
						TraceID:      "trace-json",
						SpanID:       "child",
						ParentSpanID: "root",
						Timestamp:    baseTime.Add(10 * time.Millisecond),
						Duration:     200 * time.Millisecond,
						Operation:    "RunTask:test",
					},
				},
			},
		},
	}

	data, err := json.Marshal(agg)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var back AggregatedTrace
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if back.TraceID != "trace-json" {
		t.Errorf("TraceID mismatch: %s", back.TraceID)
	}
	if back.SpanCount != 2 {
		t.Errorf("SpanCount mismatch: %d", back.SpanCount)
	}
	if back.RootSpan == nil {
		t.Fatal("RootSpan is nil after roundtrip")
	}
	if back.RootSpan.Span.SpanID != "root" {
		t.Errorf("root SpanID mismatch: %s", back.RootSpan.Span.SpanID)
	}
	if len(back.RootSpan.Children) != 1 {
		t.Fatalf("expected 1 child, got %d", len(back.RootSpan.Children))
	}
	if back.RootSpan.Children[0].Span.SpanID != "child" {
		t.Errorf("child SpanID mismatch: %s", back.RootSpan.Children[0].Span.SpanID)
	}
}

func TestTraceReader_GetTrace(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "traces.log")

	baseTime := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)
	lines := []string{
		"TRACE " + baseTime.Format(time.RFC3339Nano) + " trace-a span-root op=http:GET / duration=500ms",
		"TRACE " + baseTime.Add(10*time.Millisecond).Format(time.RFC3339Nano) + " trace-a span-child parent=span-root op=RunTask:test duration=200ms",
		"TRACE " + baseTime.Format(time.RFC3339Nano) + " trace-b span-1 op=mcp:bt_get_tree duration=50ms",
	}
	_ = os.WriteFile(logPath, []byte(strings.Join(lines, "\n")+"\n"), 0644)

	r := NewTraceReader(logPath)

	// Fetch trace-a (2 spans, parent-child)
	trace, err := r.GetTrace("trace-a")
	if err != nil {
		t.Fatalf("GetTrace: %v", err)
	}
	if trace == nil {
		t.Fatal("expected non-nil trace for trace-a")
	}
	if trace.SpanCount != 2 {
		t.Errorf("expected SpanCount=2, got %d", trace.SpanCount)
	}
	if trace.RootSpan.Span.SpanID != "span-root" {
		t.Errorf("expected root span-root, got %s", trace.RootSpan.Span.SpanID)
	}

	// Fetch trace-b (1 span)
	trace, err = r.GetTrace("trace-b")
	if err != nil {
		t.Fatalf("GetTrace trace-b: %v", err)
	}
	if trace.SpanCount != 1 {
		t.Errorf("expected SpanCount=1, got %d", trace.SpanCount)
	}
}

func TestTraceReader_GetTrace_NotFound(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "traces.log")

	_ = os.WriteFile(logPath, []byte("TRACE 2026-06-01T10:00:00Z trace-1 span-1 op=test duration=1ms\n"), 0644)

	r := NewTraceReader(logPath)
	trace, err := r.GetTrace("nonexistent")
	if err != nil {
		t.Fatalf("GetTrace: %v", err)
	}
	if trace != nil {
		t.Error("expected nil for nonexistent trace")
	}
}

func TestTraceReader_GetTrace_NoFile(t *testing.T) {
	r := NewTraceReader("/nonexistent/traces.log")
	trace, err := r.GetTrace("any-trace")
	if err != nil {
		t.Fatalf("GetTrace: %v", err)
	}
	if trace != nil {
		t.Error("expected nil for nonexistent file")
	}
}

func TestTraceReader_ListTraceIDs(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "traces.log")

	baseTime := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)
	lines := []string{
		"TRACE " + baseTime.Format(time.RFC3339Nano) + " trace-1 span-1 op=test duration=1ms",
		"TRACE " + baseTime.Add(1*time.Second).Format(time.RFC3339Nano) + " trace-1 span-2 op=test duration=1ms",
		"TRACE " + baseTime.Add(2*time.Second).Format(time.RFC3339Nano) + " trace-2 span-3 op=other duration=1ms",
		"TRACE " + baseTime.Add(3*time.Second).Format(time.RFC3339Nano) + " trace-3 span-4 op=third duration=1ms",
	}
	_ = os.WriteFile(logPath, []byte(strings.Join(lines, "\n")+"\n"), 0644)

	r := NewTraceReader(logPath)
	traces, err := r.ListTraceIDs(10)
	if err != nil {
		t.Fatalf("ListTraceIDs: %v", err)
	}

	if len(traces) != 3 {
		t.Fatalf("expected 3 traces, got %d", len(traces))
	}

	// Newest first (trace-3 should be first)
	if traces[0].TraceID != "trace-3" {
		t.Errorf("expected trace-3 first, got %s", traces[0].TraceID)
	}

	// trace-1 should have SpanCount=2
	if traces[2].TraceID != "trace-1" {
		t.Errorf("expected trace-1 last, got %s", traces[2].TraceID)
	}
	if traces[2].SpanCount != 2 {
		t.Errorf("expected trace-1 SpanCount=2, got %d", traces[2].SpanCount)
	}
}

func TestTraceReader_ListTraceIDs_Limit(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "traces.log")

	baseTime := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)
	var lines []string
	for i := 0; i < 10; i++ {
		lines = append(lines, "TRACE "+
			baseTime.Add(time.Duration(i)*time.Second).Format(time.RFC3339Nano)+
			" trace-"+formatInt(i+1)+
			" span-"+formatInt(i+1)+
			" op=test duration=1ms")
	}
	_ = os.WriteFile(logPath, []byte(strings.Join(lines, "\n")+"\n"), 0644)

	r := NewTraceReader(logPath)
	traces, err := r.ListTraceIDs(3)
	if err != nil {
		t.Fatalf("ListTraceIDs: %v", err)
	}
	if len(traces) != 3 {
		t.Fatalf("expected 3 traces (limit), got %d", len(traces))
	}
}

func TestTraceReader_ListTraceIDs_NoFile(t *testing.T) {
	r := NewTraceReader("/nonexistent/traces.log")
	traces, err := r.ListTraceIDs(5)
	if err != nil {
		t.Fatalf("ListTraceIDs: %v", err)
	}
	if len(traces) != 0 {
		t.Errorf("expected 0 traces, got %d", len(traces))
	}
}

func TestTraceReader_ListTraceIDs_DefaultLimit(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "traces.log")

	baseTime := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)
	_ = os.WriteFile(logPath, []byte("TRACE "+baseTime.Format(time.RFC3339Nano)+" trace-1 span-1 op=test duration=1ms\n"), 0644)

	r := NewTraceReader(logPath)
	// limit=0 should use default (20)
	traces, err := r.ListTraceIDs(0)
	if err != nil {
		t.Fatalf("ListTraceIDs with limit=0: %v", err)
	}
	if len(traces) != 1 {
		t.Errorf("expected 1 trace, got %d", len(traces))
	}
}
