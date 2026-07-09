package dashboard

import (
	"net/http/httptest"
	"runtime/debug"
	"strings"
	"testing"
)

// scrapeMetrics renders the Prometheus exposition and returns the body.
func scrapeMetrics(t *testing.T) string {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/metrics", nil)
	PrometheusHandler().ServeHTTP(rec, req)
	return rec.Body.String()
}

// TestNodeDurationHistogramSeriesRendered asserts that observed BT node
// durations are exposed as a full Prometheus histogram series
// (_bucket/_sum/_count) with cumulative per-bucket counts, so alerting can
// query latency distributions.
func TestNodeDurationHistogramSeriesRendered(t *testing.T) {
	// Unique labels isolate this test from other observations into the
	// shared global histogram.
	RecordNodeTick("Action", "prom_node_hist", "Parent", "", "success", 3)
	RecordNodeTick("Action", "prom_node_hist", "Parent", "", "success", 40)

	body := scrapeMetrics(t)

	if !strings.Contains(body, "# TYPE bt_node_duration_ms histogram\n") {
		t.Errorf("missing TYPE header for bt_node_duration_ms histogram in exposition:\n%s", body)
	}

	// Node bounds include 5 and 50; observations 3ms and 40ms must render
	// cumulatively: le="5" -> 1, le="50" -> 2, le="+Inf" -> 2.
	for _, want := range []string{
		`bt_node_duration_ms_bucket{le="5",name="prom_node_hist",type="Action"} 1`,
		`bt_node_duration_ms_bucket{le="50",name="prom_node_hist",type="Action"} 2`,
		`bt_node_duration_ms_bucket{le="+Inf",name="prom_node_hist",type="Action"} 2`,
		`bt_node_duration_ms_sum{name="prom_node_hist",type="Action"} 43`,
		`bt_node_duration_ms_count{name="prom_node_hist",type="Action"} 2`,
	} {
		if !strings.Contains(body, want+"\n") {
			t.Errorf("missing line %q in /metrics exposition", want)
		}
	}
}

// TestAgentTaskDurationHistogramSeriesRendered asserts that RecordTask
// observes per-agent task duration into a labeled histogram exported as
// bt_agent_task_duration_ms (full _bucket/_sum/_count series), alongside the
// existing bt_agent_duration_ms_total counter, so latency-percentile alerts
// can be defined per agent.
func TestAgentTaskDurationHistogramSeriesRendered(t *testing.T) {
	// Unique agent name isolates this test from other RecordTask callers
	// into the shared global metrics.
	RecordTask("prom_agent_hist", true, 3)
	RecordTask("prom_agent_hist", false, 40)

	body := scrapeMetrics(t)

	if !strings.Contains(body, "# TYPE bt_agent_task_duration_ms histogram\n") {
		t.Errorf("missing TYPE header for bt_agent_task_duration_ms histogram in exposition:\n%s", body)
	}

	// Bounds must include 5 and 50; observations 3ms and 40ms must render
	// cumulatively: le="5" -> 1, le="50" -> 2, le="+Inf" -> 2.
	for _, want := range []string{
		`bt_agent_task_duration_ms_bucket{agent="prom_agent_hist",le="5"} 1`,
		`bt_agent_task_duration_ms_bucket{agent="prom_agent_hist",le="50"} 2`,
		`bt_agent_task_duration_ms_bucket{agent="prom_agent_hist",le="+Inf"} 2`,
		`bt_agent_task_duration_ms_sum{agent="prom_agent_hist"} 43`,
		`bt_agent_task_duration_ms_count{agent="prom_agent_hist"} 2`,
	} {
		if !strings.Contains(body, want+"\n") {
			t.Errorf("missing line %q in /metrics exposition", want)
		}
	}

	// The existing total-duration counter must keep rendering unchanged.
	if want := `bt_agent_duration_ms_total{agent="prom_agent_hist"} 43`; !strings.Contains(body, want+"\n") {
		t.Errorf("missing line %q in /metrics exposition", want)
	}
}

// TestBlockDurationHistogramSeriesRendered asserts the same full histogram
// series for block operation durations.
func TestBlockDurationHistogramSeriesRendered(t *testing.T) {
	RecordBlockOp("expand", "prom_block_hist", "ok", 3)
	RecordBlockOp("expand", "prom_block_hist", "ok", 40)

	body := scrapeMetrics(t)

	if !strings.Contains(body, "# TYPE bt_block_duration_ms histogram\n") {
		t.Errorf("missing TYPE header for bt_block_duration_ms histogram in exposition:\n%s", body)
	}

	// Block bounds include 5 and 50; observations 3ms and 40ms must render
	// cumulatively: le="5" -> 1, le="50" -> 2, le="+Inf" -> 2.
	for _, want := range []string{
		`bt_block_duration_ms_bucket{block_id="prom_block_hist",le="5",operation="expand"} 1`,
		`bt_block_duration_ms_bucket{block_id="prom_block_hist",le="50",operation="expand"} 2`,
		`bt_block_duration_ms_bucket{block_id="prom_block_hist",le="+Inf",operation="expand"} 2`,
		`bt_block_duration_ms_sum{block_id="prom_block_hist",operation="expand"} 43`,
		`bt_block_duration_ms_count{block_id="prom_block_hist",operation="expand"} 2`,
	} {
		if !strings.Contains(body, want+"\n") {
			t.Errorf("missing line %q in /metrics exposition", want)
		}
	}
}

// TestBuildIdentityFromBuildInfo pins the parsing of runtime/debug build info
// into the platform's BuildIdentity: vcs.revision, vcs.time, and vcs.modified
// become Revision, CommitTime, and Dirty. Binaries built without VCS stamping
// (-buildvcs=false, test binaries, tarball builds) and a failed ReadBuildInfo
// (nil) must degrade to the "unknown" sentinel instead of empty strings, so
// the startup log line and the bt_build_info gauge never carry an empty
// revision label that Prometheus queries would silently match everything on.
func TestBuildIdentityFromBuildInfo(t *testing.T) {
	bi := &debug.BuildInfo{Settings: []debug.BuildSetting{
		{Key: "vcs.revision", Value: "abc1234def5678"},
		{Key: "vcs.time", Value: "2026-07-08T12:00:00Z"},
		{Key: "vcs.modified", Value: "true"},
	}}
	id := BuildIdentityFromBuildInfo(bi)
	if id.Revision != "abc1234def5678" {
		t.Errorf("Revision = %q, want %q (from vcs.revision)", id.Revision, "abc1234def5678")
	}
	if id.CommitTime != "2026-07-08T12:00:00Z" {
		t.Errorf("CommitTime = %q, want %q (from vcs.time)", id.CommitTime, "2026-07-08T12:00:00Z")
	}
	if !id.Dirty {
		t.Error("Dirty = false, want true (vcs.modified=true)")
	}

	clean := BuildIdentityFromBuildInfo(&debug.BuildInfo{Settings: []debug.BuildSetting{
		{Key: "vcs.revision", Value: "abc1234def5678"},
		{Key: "vcs.modified", Value: "false"},
	}})
	if clean.Dirty {
		t.Error("Dirty = true, want false (vcs.modified=false)")
	}

	unstamped := BuildIdentityFromBuildInfo(&debug.BuildInfo{})
	if unstamped.Revision != "unknown" {
		t.Errorf("unstamped Revision = %q, want %q sentinel", unstamped.Revision, "unknown")
	}
	if unstamped.CommitTime != "unknown" {
		t.Errorf("unstamped CommitTime = %q, want %q sentinel", unstamped.CommitTime, "unknown")
	}
	if unstamped.Dirty {
		t.Error("unstamped Dirty = true, want false")
	}

	nilID := BuildIdentityFromBuildInfo(nil)
	if nilID.Revision != "unknown" || nilID.CommitTime != "unknown" || nilID.Dirty {
		t.Errorf("nil build info must yield the unknown sentinel identity, got %+v", nilID)
	}
}

// TestPrometheusBuildInfoGaugeRenderedAndReplaced pins the bt_build_info gauge
// on /metrics — the machine-checkable side of the stale-daemon-binary drift
// detection (three incidents to date were diagnosed via DLQ-message
// heuristics). The exposition must always carry exactly one
// bt_build_info{dirty,revision} series set to 1: present by default (the
// serving process self-identifies via ReadBuildInfo without any main-package
// wiring), pinned to the exact identity after SetBuildIdentity, and REPLACED
// — not accumulated — when the identity is set again, so a scrape comparing
// the running revision against repo HEAD can never match a stale leftover
// series.
func TestPrometheusBuildInfoGaugeRenderedAndReplaced(t *testing.T) {
	// Default: the gauge is present before any explicit SetBuildIdentity call,
	// because writePrometheusMetrics self-populates from the process build info.
	body := scrapeMetrics(t)
	if !strings.Contains(body, "# TYPE bt_build_info gauge\n") {
		t.Errorf("missing TYPE header for bt_build_info gauge in exposition:\n%s", body)
	}
	if !strings.Contains(body, "bt_build_info{") {
		t.Error("bt_build_info series must be exposed by default (self-identified via ReadBuildInfo), none found")
	}

	SetBuildIdentity(BuildIdentity{Revision: "abc1234def5678", CommitTime: "2026-07-08T12:00:00Z", Dirty: true})
	body = scrapeMetrics(t)
	want := `bt_build_info{dirty="true",revision="abc1234def5678"} 1`
	if !strings.Contains(body, want+"\n") {
		t.Errorf("missing line %q in /metrics exposition after SetBuildIdentity", want)
	}

	// A second Set (new binary identity) must replace the series, not add one.
	SetBuildIdentity(BuildIdentity{Revision: "fedcba9876543", CommitTime: "2026-07-08T13:00:00Z", Dirty: false})
	body = scrapeMetrics(t)
	want = `bt_build_info{dirty="false",revision="fedcba9876543"} 1`
	if !strings.Contains(body, want+"\n") {
		t.Errorf("missing line %q in /metrics exposition after second SetBuildIdentity", want)
	}
	if strings.Contains(body, `revision="abc1234def5678"`) {
		t.Error("stale bt_build_info series from the previous identity still exposed after SetBuildIdentity")
	}
	if n := strings.Count(body, "bt_build_info{"); n != 1 {
		t.Errorf("exposition carries %d bt_build_info series, want exactly 1", n)
	}
}

// TestKGAnalyticsGaugesRenderedAndUpdated pins the analytics→observability link
// (milestone 4/4): ComputeAnalytics signals must be visible in Prometheus, not
// only in the text report bt_kg_analytics returns. RecordKGAnalytics publishes
// the coverage-gap, bottleneck, and selection-pressure counts as three plain
// gauges, and re-recording REPLACES the values (gauges track the latest
// analytics run, never accumulate) so a scrape reflects the current graph
// health rather than a running sum across scrapes.
func TestKGAnalyticsGaugesRenderedAndUpdated(t *testing.T) {
	RecordKGAnalytics(3, 5, 7)

	body := scrapeMetrics(t)

	for _, header := range []string{
		"# TYPE bt_kg_coverage_gaps gauge\n",
		"# TYPE bt_kg_bottlenecks gauge\n",
		"# TYPE bt_kg_selection_pressure_trees gauge\n",
	} {
		if !strings.Contains(body, header) {
			t.Errorf("missing TYPE header %q in /metrics exposition:\n%s", header, body)
		}
	}

	for _, want := range []string{
		"bt_kg_coverage_gaps 3",
		"bt_kg_bottlenecks 5",
		"bt_kg_selection_pressure_trees 7",
	} {
		if !strings.Contains(body, want+"\n") {
			t.Errorf("missing line %q in /metrics exposition", want)
		}
	}

	// A second analytics run must replace the gauge values, not accumulate.
	RecordKGAnalytics(1, 0, 4)
	body = scrapeMetrics(t)
	for _, want := range []string{
		"bt_kg_coverage_gaps 1",
		"bt_kg_bottlenecks 0",
		"bt_kg_selection_pressure_trees 4",
	} {
		if !strings.Contains(body, want+"\n") {
			t.Errorf("missing updated line %q in /metrics exposition", want)
		}
	}
	for _, stale := range []string{
		"bt_kg_coverage_gaps 3\n",
		"bt_kg_bottlenecks 5\n",
		"bt_kg_selection_pressure_trees 7\n",
	} {
		if strings.Contains(body, stale) {
			t.Errorf("stale gauge line %q still exposed after second RecordKGAnalytics", strings.TrimSuffix(stale, "\n"))
		}
	}
}

// TestReadBuildIdentityNeverEmpty pins that ReadBuildIdentity — the helper the
// long-running binaries log at startup — never returns empty identity fields,
// whatever the build environment stamped (test binaries typically carry no
// vcs settings).
func TestReadBuildIdentityNeverEmpty(t *testing.T) {
	id := ReadBuildIdentity()
	if id.Revision == "" {
		t.Error("ReadBuildIdentity().Revision is empty, want a revision or the \"unknown\" sentinel")
	}
	if id.CommitTime == "" {
		t.Error("ReadBuildIdentity().CommitTime is empty, want a timestamp or the \"unknown\" sentinel")
	}
}
