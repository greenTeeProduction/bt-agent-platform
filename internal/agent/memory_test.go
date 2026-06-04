package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func storeForTest(t *testing.T, agentName string, maxSize int) *MemoryStore {
	t.Helper()
	ms, err := NewMemoryStore(t.TempDir(), agentName, maxSize)
	if err != nil {
		t.Fatalf("NewMemoryStore(test, %q, %d): %v", agentName, maxSize, err)
	}
	return ms
}

func TestNewMemoryStore_CreatesDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "new-store")
	ms, err := NewMemoryStore(dir, "test-agent", 100)
	if err != nil {
		t.Fatalf("NewMemoryStore: %v", err)
	}
	if ms == nil {
		t.Fatal("expected non-nil store")
	}
	if ms.maxSize != 100 {
		t.Fatalf("expected maxSize=100, got %d", ms.maxSize)
	}
	if _, err := os.Stat(dir + "/test-agent"); os.IsNotExist(err) {
		t.Fatal("expected agent dir to be created")
	}
}

func TestNewMemoryStore_LoadsExistingData(t *testing.T) {
	dir := t.TempDir()
	// Create and populate
	ms1, err := NewMemoryStore(dir, "load-test", 100)
	if err != nil {
		t.Fatal(err)
	}
	if err := ms1.Write("key1", "val1", "fact", "high", "manual"); err != nil {
		t.Fatal(err)
	}

	// Re-open and verify
	ms2, err := NewMemoryStore(dir, "load-test", 100)
	if err != nil {
		t.Fatalf("re-open: %v", err)
	}
	if got := ms2.Read("key1"); got != "val1" {
		t.Fatalf("expected val1, got %q", got)
	}
}

func TestNewMemoryStore_NonExistentDirIsOK(t *testing.T) {
	ms, err := NewMemoryStore(t.TempDir(), "fresh-agent", 50)
	if err != nil {
		t.Fatalf("NewMemoryStore on fresh dir: %v", err)
	}
	if ms == nil {
		t.Fatal("expected non-nil store")
	}
}

func TestWrite_CreatesNewEntry(t *testing.T) {
	ms := storeForTest(t, "write-create", 50)
	if err := ms.Write("fact:test", "hello world", "fact", "high", "agent"); err != nil {
		t.Fatalf("Write: %v", err)
	}
	got := ms.Read("fact:test")
	if got != "hello world" {
		t.Fatalf("expected 'hello world', got %q", got)
	}
}

func TestWrite_UpdatesExistingEntry(t *testing.T) {
	ms := storeForTest(t, "write-update", 50)
	if err := ms.Write("k", "v1", "fact", "high", "manual"); err != nil {
		t.Fatal(err)
	}
	if err := ms.Write("k", "v2", "", "", ""); err != nil {
		t.Fatal(err)
	}
	got := ms.Read("k")
	if got != "v2" {
		t.Fatalf("expected 'v2', got %q", got)
	}
}

func TestWrite_UpdateDoesNotOverwriteCategory(t *testing.T) {
	ms := storeForTest(t, "write-update-cat", 50)
	_ = ms.Write("k", "v1", "fact", "high", "manual")
	_ = ms.Write("k", "v2", "", "", "") // empty category/priority should not overwrite
	results := ms.Query("fact", "", 0)
	if len(results) != 1 {
		t.Fatalf("expected 1 result with category=fact, got %d", len(results))
	}
}

func TestWrite_EvictsOldestWhenFull(t *testing.T) {
	ms := storeForTest(t, "write-evict", 3)
	_ = ms.Write("a", "1", "fact", "low", "test")
	time.Sleep(1 * time.Millisecond) // ensure different timestamps
	_ = ms.Write("b", "2", "fact", "low", "test")
	time.Sleep(1 * time.Millisecond)
	_ = ms.Write("c", "3", "fact", "low", "test")
	// Now a 4th write should evict "a" (oldest)
	_ = ms.Write("d", "4", "fact", "low", "test")

	stats := ms.Stats()
	if stats["total"] != 3 {
		t.Fatalf("expected 3 entries after eviction, got %d", stats["total"])
	}
	if ms.Read("a") != "" {
		t.Fatal("expected 'a' to be evicted (oldest)")
	}
	if ms.Read("d") != "4" {
		t.Fatal("expected 'd' to exist")
	}
}

func TestWrite_EvictAndAddOnMaxSize1(t *testing.T) {
	// maxSize=1 — write k1, then k2 should evict k1 and succeed
	ms := storeForTest(t, "write-full", 1)
	if err := ms.Write("k1", "v1", "fact", "high", "test"); err != nil {
		t.Fatal(err)
	}
	err := ms.Write("k2", "v2", "fact", "high", "test")
	if err != nil {
		t.Fatalf("expected second write to succeed after eviction, got: %v", err)
	}
	if ms.Read("k2") != "v2" {
		t.Fatal("k2 should exist")
	}
	if ms.Read("k1") != "" {
		t.Fatal("k1 should have been evicted")
	}
}

func TestRead_ReturnsEmptyForMissing(t *testing.T) {
	ms := storeForTest(t, "read-missing", 50)
	if got := ms.Read("nonexistent"); got != "" {
		t.Fatalf("expected empty string, got %q", got)
	}
}

func TestRead_IncrementsHitCount(t *testing.T) {
	ms := storeForTest(t, "read-hit", 50)
	_ = ms.Write("k", "v", "fact", "high", "test")
	ms.Read("k")
	ms.Read("k")
	ms.Read("k")

	results := ms.Query("fact", "", 0)
	for _, r := range results {
		if r.Key == "k" && r.HitCount != 3 {
			t.Fatalf("expected HitCount=3, got %d", r.HitCount)
		}
	}
}

func TestQuery_ByCategory(t *testing.T) {
	ms := storeForTest(t, "query-cat", 100)
	_ = ms.Write("fact:a", "1", "fact", "high", "test")
	_ = ms.Write("pitfall:err", "2", "pitfall", "medium", "test")
	_ = ms.Write("pattern:ok", "3", "pattern", "low", "test")

	facts := ms.Query("fact", "", 0)
	if len(facts) != 1 {
		t.Fatalf("expected 1 fact, got %d", len(facts))
	}
}

func TestQuery_ByPriority(t *testing.T) {
	ms := storeForTest(t, "query-pri", 100)
	_ = ms.Write("a", "1", "fact", "high", "test")
	_ = ms.Write("b", "2", "fact", "medium", "test")
	_ = ms.Write("c", "3", "fact", "low", "test")

	high := ms.Query("", "high", 0)
	if len(high) != 1 {
		t.Fatalf("expected 1 high-priority entry, got %d", len(high))
	}
}

func TestQuery_ByCategoryAndPriority(t *testing.T) {
	ms := storeForTest(t, "query-cat-pri", 100)
	_ = ms.Write("a", "1", "fact", "high", "test")
	_ = ms.Write("b", "2", "fact", "medium", "test")
	_ = ms.Write("c", "3", "pitfall", "high", "test")

	results := ms.Query("fact", "high", 0)
	if len(results) != 1 {
		t.Fatalf("expected 1 (fact+high), got %d", len(results))
	}
}

func TestQuery_SortsByPriorityThenRecency(t *testing.T) {
	ms := storeForTest(t, "query-sort", 100)
	_ = ms.Write("a", "1", "fact", "low", "test")
	time.Sleep(1 * time.Millisecond)
	_ = ms.Write("b", "2", "fact", "high", "test")
	time.Sleep(1 * time.Millisecond)
	_ = ms.Write("c", "3", "fact", "medium", "test")

	results := ms.Query("fact", "", 0)
	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}
	// high first, then medium, then low
	if results[0].Key != "b" || results[1].Key != "c" || results[2].Key != "a" {
		t.Fatalf("expected order b(c),c(m),a(l), got %s,%s,%s", results[0].Key, results[1].Key, results[2].Key)
	}
}

func TestQuery_LimitResults(t *testing.T) {
	ms := storeForTest(t, "query-limit", 100)
	for i := 0; i < 10; i++ {
		key := fmt.Sprintf("k%d", i)
		_ = ms.Write(key, "v", "fact", "low", "test")
	}
	results := ms.Query("", "", 3)
	if len(results) != 3 {
		t.Fatalf("expected 3 results with limit=3, got %d", len(results))
	}
}

func TestQuery_ReturnsAllWhenNoFilter(t *testing.T) {
	ms := storeForTest(t, "query-all", 100)
	for i := 0; i < 5; i++ {
		_ = ms.Write(fmt.Sprintf("k%d", i), "v", "fact", "low", "test")
	}
	results := ms.Query("", "", 0)
	if len(results) != 5 {
		t.Fatalf("expected 5 results, got %d", len(results))
	}
}

func TestContextBlock_EmptyStoreReturnsEmpty(t *testing.T) {
	ms := storeForTest(t, "ctx-empty", 50)
	if got := ms.ContextBlock(); got != "" {
		t.Fatalf("expected empty, got %q", got)
	}
}

func TestContextBlock_IncludesFactsPitfallsPatterns(t *testing.T) {
	ms := storeForTest(t, "ctx-full", 50)
	_ = ms.Write("fact:test_key", "test_value_123", "fact", "high", "manual")
	_ = ms.Write("pitfall:dont_do", "do_not_do_this_thing", "pitfall", "high", "agent")
	_ = ms.Write("pattern:good", "do_this_instead", "pattern", "high", "extracted")

	block := ms.ContextBlock()
	if !strings.Contains(block, "test_key") {
		t.Fatal("expected test_key in context block")
	}
	if !strings.Contains(block, "dont_do") {
		t.Fatal("expected pitfall in context block")
	}
	if !strings.Contains(block, "good") {
		t.Fatal("expected pattern in context block")
	}
	if !strings.Contains(block, "AGENT MEMORY") {
		t.Fatal("expected AGENT MEMORY header")
	}
}

func TestContextBlock_SkipsNonHighPriorityItems(t *testing.T) {
	ms := storeForTest(t, "ctx-skip", 50)
	_ = ms.Write("fact:a", "low_priority_value", "fact", "low", "test")
	block := ms.ContextBlock()
	if strings.Contains(block, "low_priority_value") {
		t.Fatal("expected low-priority fact to not appear in ContextBlock")
	}
}

func TestPreviousRunContext_NoHistoryReturnsEmpty(t *testing.T) {
	ms := storeForTest(t, "prev-empty", 50)
	h := &History{}
	got := ms.PreviousRunContext(h, "test-agent", 3)
	if got != "" {
		t.Fatalf("expected empty, got %q", got)
	}
}

func TestPreviousRunContext_ReturnsSuccessfulRuns(t *testing.T) {
	ms := storeForTest(t, "prev-success", 50)
	h, err := NewHistory(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	_ = h.Record(RunRecord{AgentName: "test-agent", Task: "task1", Outcome: "success", Output: "output1", Duration: "100ms"})
	_ = h.Record(RunRecord{AgentName: "test-agent", Task: "task2", Outcome: "failure", Output: "", Duration: "50ms"})
	_ = h.Record(RunRecord{AgentName: "test-agent", Task: "task3", Outcome: "success", Output: "output3 long output text here for testing", Duration: "200ms"})

	got := ms.PreviousRunContext(h, "test-agent", 2)
	if !strings.Contains(got, "PREVIOUS RUNS") {
		t.Fatal("expected PREVIOUS RUNS header")
	}
	if !strings.Contains(got, "task1") {
		t.Fatal("expected task1 in output")
	}
	if !strings.Contains(got, "task3") {
		t.Fatal("expected task3 in output")
	}
	// task2 was failure, should not appear
	if strings.Contains(got, "task2") {
		t.Fatal("failure task should not appear")
	}
}

func TestStats_EmptyStore(t *testing.T) {
	ms := storeForTest(t, "stats-empty", 100)
	stats := ms.Stats()
	if stats["total"] != 0 {
		t.Fatalf("expected 0 total, got %d", stats["total"])
	}
}

func TestStats_CountsByCategoryAndPriority(t *testing.T) {
	ms := storeForTest(t, "stats-full", 100)
	_ = ms.Write("a", "1", "fact", "high", "test")
	_ = ms.Write("b", "2", "fact", "high", "test")
	_ = ms.Write("c", "3", "pitfall", "medium", "test")

	stats := ms.Stats()
	if stats["total"] != 3 {
		t.Fatalf("expected total=3, got %d", stats["total"])
	}
	if stats["fact"] != 2 {
		t.Fatalf("expected fact=2, got %d", stats["fact"])
	}
	if stats["pitfall"] != 1 {
		t.Fatalf("expected pitfall=1, got %d", stats["pitfall"])
	}
	if stats["priority_high"] != 2 {
		t.Fatalf("expected priority_high=2, got %d", stats["priority_high"])
	}
	if stats["priority_medium"] != 1 {
		t.Fatalf("expected priority_medium=1, got %d", stats["priority_medium"])
	}
}

func TestDelete_RemovesEntry(t *testing.T) {
	ms := storeForTest(t, "delete-ok", 100)
	_ = ms.Write("k", "v", "fact", "high", "test")
	if ok := ms.Delete("k"); !ok {
		t.Fatal("expected Delete to return true")
	}
	if ms.Read("k") != "" {
		t.Fatal("expected key to be deleted")
	}
}

func TestDelete_ReturnsFalseForMissing(t *testing.T) {
	ms := storeForTest(t, "delete-miss", 100)
	if ok := ms.Delete("nonexistent"); ok {
		t.Fatal("expected Delete to return false for missing key")
	}
}

func TestDelete_PersistsAfterReopen(t *testing.T) {
	baseDir := t.TempDir()
	ms, err := NewMemoryStore(baseDir, "delete-persist", 100)
	if err != nil {
		t.Fatal(err)
	}
	_ = ms.Write("k", "v", "fact", "high", "test")
	ms.Delete("k")

	// Reopen and verify
	ms2, err := NewMemoryStore(baseDir, "delete-persist", 100)
	if err != nil {
		t.Fatal(err)
	}
	if ms2.Read("k") != "" {
		t.Fatal("deletion should persist across reopen")
	}
}

func TestPriorityWeight(t *testing.T) {
	tests := []struct {
		input string
		want  int
	}{
		{"high", 3},
		{"medium", 2},
		{"low", 1},
		{"unknown", 0},
		{"", 0},
	}
	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			if got := priorityWeight(tc.input); got != tc.want {
				t.Fatalf("priorityWeight(%q) = %d, want %d", tc.input, got, tc.want)
			}
		})
	}
}

func TestSummarizeOutput(t *testing.T) {
	tests := []struct {
		input  string
		maxLen int
		want   string
	}{
		{"hello world", 50, "hello world"},
		{"first para\n\nsecond para", 50, "first para"},
		{"line one\n\nrest", 5, "li..."}, // truncated "line one" to 5 chars → "li..."
		{"", 50, ""},
	}
	for _, tc := range tests {
		name := tc.input
		if len(name) > 10 {
			name = name[:10]
		}
		t.Run(name, func(t *testing.T) {
			got := summarizeOutput(tc.input, tc.maxLen)
			if got != tc.want {
				t.Fatalf("summarizeOutput(%q, %d) = %q, want %q", tc.input, tc.maxLen, got, tc.want)
			}
		})
	}
}

func TestQueryWithCategoryPrefix(t *testing.T) {
	ms := storeForTest(t, "query-prefix", 50)
	_ = ms.Write("fact:a", "1", "fact_alpha", "high", "test")
	_ = ms.Write("fact:b", "2", "fact_beta", "high", "test")
	_ = ms.Write("pitfall:x", "3", "pitfall", "high", "test")

	// Query with prefix "fact_" should match fact_alpha and fact_beta
	results := ms.Query("fact_", "", 0)
	if len(results) != 2 {
		t.Fatalf("expected 2 results with prefix 'fact_', got %d", len(results))
	}
}

func TestEvictLRU_RemovesOldestEntry(t *testing.T) {
	ms := storeForTest(t, "evict-lru", 10)
	_ = ms.Write("old", "1", "fact", "low", "test")
	time.Sleep(1 * time.Millisecond)
	_ = ms.Write("new", "2", "fact", "high", "test")

	// evictLRU is unexported; trigger it via full store
	// Write to a full store to trigger eviction
	smallStore := storeForTest(t, "evict-small", 2)
	_ = smallStore.Write("first", "a", "fact", "low", "test")
	time.Sleep(1 * time.Millisecond)
	_ = smallStore.Write("second", "b", "fact", "low", "test")
	time.Sleep(1 * time.Millisecond)
	// This should evict "first"
	_ = smallStore.Write("third", "c", "fact", "low", "test")

	if smallStore.Read("first") != "" {
		t.Fatal("expected 'first' to be evicted (LRU)")
	}
	if smallStore.Read("third") != "c" {
		t.Fatal("expected 'third' to exist")
	}
}
