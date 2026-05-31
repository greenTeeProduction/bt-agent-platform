package tracing

import (
	"bufio"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"
)

// ─── TraceEntry ───────────────────────────────────────────────────────────────

// TraceEntry represents a single parsed trace line from the traces log.
type TraceEntry struct {
	Timestamp    time.Time         `json:"timestamp"`
	TraceID      string            `json:"trace_id"`
	SpanID       string            `json:"span_id"`
	ParentSpanID string            `json:"parent_span_id,omitempty"`
	Operation    string            `json:"operation"`
	Duration     time.Duration     `json:"duration"`
	DurationMS   int64             `json:"duration_ms"`
	Attributes   map[string]string `json:"attributes,omitempty"`
	Events       []TraceEvent      `json:"events,omitempty"`
	Error        string            `json:"error,omitempty"`
}

// TraceEvent represents a timestamped event within a trace span.
type TraceEvent struct {
	Name string            `json:"name"`
	Attrs map[string]string `json:"attrs,omitempty"`
}

// traceLineRE matches the ConsoleTracer output format.
// TRACE <ts> <trace_id> <span_id> [parent=<parent>] op=<name...> duration=<dur> [key=value...] [[event attrs...]] [error="..."]
// The op value can contain spaces — it runs until " duration=" is found.
var traceLineRE = regexp.MustCompile(
	`^TRACE\s+` +
		`(\S+)` + // timestamp
		`\s+(\S+)` + // trace_id
		`\s+(\S+)` + // span_id
		`(?:\s+parent=(\S+))?` + // optional parent
		`\s+op=(.+?)` + // operation name (may contain spaces, non-greedy until " duration=")
		`\s+duration=(\S+)`, // duration
)

// ParseTraceLine parses a single ConsoleTracer output line into a TraceEntry.
// Returns nil if the line is not a valid trace.
func ParseTraceLine(line string) *TraceEntry {
	line = strings.TrimSpace(line)
	if line == "" {
		return nil
	}

	matches := traceLineRE.FindStringSubmatch(line)
	if len(matches) < 7 {
		return nil
	}

	// Parse timestamp — try RFC3339Nano first
	ts, err := time.Parse(time.RFC3339Nano, matches[1])
	if err != nil {
		// Fallback: some timestamps may use space separator
		ts, err = time.Parse("2006-01-02 15:04:05.999999999-07:00", matches[1])
		if err != nil {
			return nil
		}
	}

	entry := &TraceEntry{
		Timestamp:  ts,
		TraceID:    matches[2],
		SpanID:     matches[3],
		ParentSpanID: matches[4],
		Operation:  matches[5],
		Attributes: make(map[string]string),
	}

	// Parse duration
	dur, err := time.ParseDuration(matches[6])
	if err != nil {
		return nil
	}
	entry.Duration = dur
	entry.DurationMS = dur.Milliseconds()

	// Parse remaining key=value attributes, events, and error from rest of line
	rest := line[len(matches[0]):]
	rest = strings.TrimSpace(rest)

	// Parse key=value pairs and bracketed events
	parseRemaining(entry, rest)

	return entry
}

// parseRemaining extracts key=value pairs, bracketed events, and error from the remainder.
func parseRemaining(entry *TraceEntry, rest string) {
	i := 0
	for i < len(rest) {
		// Skip whitespace
		for i < len(rest) && rest[i] == ' ' {
			i++
		}
		if i >= len(rest) {
			break
		}

		switch rest[i] {
		case '[':
			// Parse event: [event_name key=value...]
			end := strings.IndexByte(rest[i+1:], ']')
			if end < 0 {
				return
			}
			eventContent := rest[i+1 : i+1+end]
			ev := ParseTraceEvent(eventContent)
			if ev != nil {
				entry.Events = append(entry.Events, *ev)
			}
			i = i + 1 + end + 1
		default:
			// Parse key=value
			eqIdx := strings.IndexByte(rest[i:], '=')
			if eqIdx < 0 {
				return
			}
			key := rest[i : i+eqIdx]

			// Find the value — it's everything until the next space or end of string
			valStart := i + eqIdx + 1
			if valStart >= len(rest) {
				return
			}

			var val string
			if rest[valStart] == '"' {
				// Quoted value
				closeQuote := strings.IndexByte(rest[valStart+1:], '"')
				if closeQuote < 0 {
					return
				}
				val = rest[valStart+1 : valStart+1+closeQuote]
				i = valStart + 1 + closeQuote + 1
			} else {
				// Unquoted value — until next space
				nextSpace := strings.IndexByte(rest[valStart:], ' ')
				if nextSpace < 0 {
					val = rest[valStart:]
					i = len(rest)
				} else {
					val = rest[valStart : valStart+nextSpace]
					i = valStart + nextSpace
				}
			}

			if key == "error" {
				entry.Error = val
			} else {
				entry.Attributes[key] = val
			}
		}
	}
}

// ParseTraceEvent parses an event body like "slow_request threshold=5s actual=5.1s"
func ParseTraceEvent(body string) *TraceEvent {
	body = strings.TrimSpace(body)
	if body == "" {
		return nil
	}

	// First token is the event name
	firstSpace := strings.IndexByte(body, ' ')
	if firstSpace < 0 {
		return &TraceEvent{Name: body, Attrs: make(map[string]string)}
	}

	ev := &TraceEvent{
		Name:  body[:firstSpace],
		Attrs: make(map[string]string),
	}

	// Parse remaining key=value pairs
	rest := body[firstSpace+1:]
	for len(rest) > 0 {
		rest = strings.TrimSpace(rest)
		if rest == "" {
			break
		}
		eqIdx := strings.IndexByte(rest, '=')
		if eqIdx < 0 {
			break
		}
		key := rest[:eqIdx]
		rest = rest[eqIdx+1:]

		nextSpace := strings.IndexByte(rest, ' ')
		var val string
		if nextSpace < 0 {
			val = rest
			rest = ""
		} else {
			val = rest[:nextSpace]
			rest = rest[nextSpace+1:]
		}
		ev.Attrs[key] = val
	}

	return ev
}

// ─── TraceReader ──────────────────────────────────────────────────────────────

// TraceReader reads parsed trace entries from a trace log file.
type TraceReader struct {
	path string
}

// NewTraceReader creates a TraceReader for the given trace log file path.
func NewTraceReader(path string) *TraceReader {
	return &TraceReader{path: path}
}

// ReadRecent reads the most recent N trace entries from the log file.
// If the file doesn't exist, returns an empty slice with no error.
func (r *TraceReader) ReadRecent(limit int) ([]TraceEntry, error) {
	if limit <= 0 {
		limit = 50
	}

	f, err := os.Open(r.path)
	if err != nil {
		if os.IsNotExist(err) {
			return []TraceEntry{}, nil
		}
		return nil, fmt.Errorf("open trace log: %w", err)
	}
	defer f.Close()

	// Read all lines
	var lines []string
	scanner := bufio.NewScanner(f)
	// Support large trace files
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan trace log: %w", err)
	}

	// Take last N lines
	start := 0
	if len(lines) > limit {
		start = len(lines) - limit
	}

	entries := make([]TraceEntry, 0, limit)
	for i := start; i < len(lines); i++ {
		entry := ParseTraceLine(lines[i])
		if entry != nil {
			entries = append(entries, *entry)
		}
	}

	// Reverse to show newest first
	for i, j := 0, len(entries)-1; i < j; i, j = i+1, j-1 {
		entries[i], entries[j] = entries[j], entries[i]
	}

	return entries, nil
}

// ReadSince reads trace entries since the given time.
// limit caps the maximum number of entries returned.
func (r *TraceReader) ReadSince(since time.Time, limit int) ([]TraceEntry, error) {
	if limit <= 0 {
		limit = 50
	}

	f, err := os.Open(r.path)
	if err != nil {
		if os.IsNotExist(err) {
			return []TraceEntry{}, nil
		}
		return nil, fmt.Errorf("open trace log: %w", err)
	}
	defer f.Close()

	entries := make([]TraceEntry, 0, limit)
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	for scanner.Scan() {
		entry := ParseTraceLine(scanner.Text())
		if entry != nil && entry.Timestamp.After(since) {
			entries = append(entries, *entry)
			if len(entries) >= limit {
				break
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan trace log: %w", err)
	}

	// Reverse to newest first
	for i, j := 0, len(entries)-1; i < j; i, j = i+1, j-1 {
		entries[i], entries[j] = entries[j], entries[i]
	}

	return entries, nil
}

// TotalLines returns the total number of lines in the trace log.
func (r *TraceReader) TotalLines() (int, error) {
	f, err := os.Open(r.path)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, fmt.Errorf("open trace log: %w", err)
	}
	defer f.Close()

	count := 0
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		count++
	}
	return count, scanner.Err()
}

// SizeBytes returns the file size of the trace log.
func (r *TraceReader) SizeBytes() (int64, error) {
	info, err := os.Stat(r.path)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	return info.Size(), nil
}

// ─── Trace Aggregation ────────────────────────────────────────────────────────

// AggregatedTrace represents a complete distributed trace assembled from individual spans.
type AggregatedTrace struct {
	TraceID      string          `json:"trace_id"`
	RootSpan     *TraceSpanNode  `json:"root_span,omitempty"`
	SpanCount    int             `json:"span_count"`
	TotalDuration time.Duration  `json:"total_duration"`
	TotalDurationMS int64        `json:"total_duration_ms"`
	StartTime    time.Time       `json:"start_time"`
	EndTime      time.Time       `json:"end_time"`
	Operations   []string        `json:"operations"`
}

// TraceSpanNode represents a span in the trace tree, with its children.
type TraceSpanNode struct {
	Span      TraceEntry       `json:"span"`
	Children  []*TraceSpanNode `json:"children,omitempty"`
}

// GetTrace reads all spans for a specific trace ID and builds the aggregated trace tree.
// Returns nil if no spans are found for the given trace ID.
func (r *TraceReader) GetTrace(traceID string) (*AggregatedTrace, error) {
	allSpans, err := r.readAllSpans()
	if err != nil {
		return nil, err
	}

	// Filter spans by trace ID
	var spans []TraceEntry
	for _, s := range allSpans {
		if s.TraceID == traceID {
			spans = append(spans, s)
		}
	}
	if len(spans) == 0 {
		return nil, nil
	}

	return buildAggregatedTrace(traceID, spans), nil
}

// ListTraceIDs returns a list of unique trace IDs with summary info, newest first.
// limit caps the number of traces returned (max 100).
func (r *TraceReader) ListTraceIDs(limit int) ([]*AggregatedTrace, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}

	allSpans, err := r.readAllSpans()
	if err != nil {
		return nil, err
	}

	// Group spans by trace ID
	byTrace := make(map[string][]TraceEntry)
	for _, s := range allSpans {
		byTrace[s.TraceID] = append(byTrace[s.TraceID], s)
	}

	// Build aggregated traces
	var traces []*AggregatedTrace
	for traceID, spans := range byTrace {
		traces = append(traces, buildAggregatedTrace(traceID, spans))
	}

	// Sort by start time descending (newest first)
	for i := 0; i < len(traces); i++ {
		for j := i + 1; j < len(traces); j++ {
			if traces[j].StartTime.After(traces[i].StartTime) {
				traces[i], traces[j] = traces[j], traces[i]
			}
		}
	}

	if len(traces) > limit {
		traces = traces[:limit]
	}

	return traces, nil
}

// readAllSpans reads and parses all trace lines from the log file.
func (r *TraceReader) readAllSpans() ([]TraceEntry, error) {
	f, err := os.Open(r.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("open trace log: %w", err)
	}
	defer f.Close()

	var spans []TraceEntry
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		entry := ParseTraceLine(scanner.Text())
		if entry != nil {
			spans = append(spans, *entry)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan trace log: %w", err)
	}
	return spans, nil
}

// buildAggregatedTrace constructs an AggregatedTrace from a set of spans with the same trace ID.
func buildAggregatedTrace(traceID string, spans []TraceEntry) *AggregatedTrace {
	if len(spans) == 0 {
		return nil
	}

	// Build span map by span ID
	spanMap := make(map[string]*TraceSpanNode)
	for i := range spans {
		spanMap[spans[i].SpanID] = &TraceSpanNode{Span: spans[i]}
	}

	// Build parent-child relationships
	var root *TraceSpanNode
	for _, node := range spanMap {
		if node.Span.ParentSpanID != "" {
			if parent, ok := spanMap[node.Span.ParentSpanID]; ok {
				parent.Children = append(parent.Children, node)
			}
		}
		// Root is the span with no parent, or the first span if no parent relationship exists
		if node.Span.ParentSpanID == "" {
			if root == nil {
				root = node
			}
		}
	}

	// If no root found (all spans have parents but parent spans weren't captured),
	// use the earliest span as root
	if root == nil && len(spanMap) > 0 {
		for _, node := range spanMap {
			if root == nil || node.Span.Timestamp.Before(root.Span.Timestamp) {
				root = node
			}
		}
		// Detach it from its parent for clean tree display
		root.Span.ParentSpanID = ""
	}

	// Compute time bounds and collect operations
	var startTime, endTime time.Time
	ops := make(map[string]bool)
	for _, s := range spans {
		if startTime.IsZero() || s.Timestamp.Before(startTime) {
			startTime = s.Timestamp
		}
		spanEnd := s.Timestamp.Add(s.Duration)
		if endTime.IsZero() || spanEnd.After(endTime) {
			endTime = spanEnd
		}
		ops[s.Operation] = true
	}

	totalDuration := endTime.Sub(startTime)
	var opList []string
	for op := range ops {
		opList = append(opList, op)
	}

	return &AggregatedTrace{
		TraceID:         traceID,
		RootSpan:        root,
		SpanCount:       len(spans),
		TotalDuration:   totalDuration,
		TotalDurationMS: totalDuration.Milliseconds(),
		StartTime:       startTime,
		EndTime:         endTime,
		Operations:      opList,
	}
}

// ─── ParseDuration helper ─────────────────────────────────────────────────────

// ParseTraceDuration parses a duration string like "151µs", "2.341s", "1m30s".
func ParseTraceDuration(s string) (time.Duration, error) {
	return time.ParseDuration(s)
}
