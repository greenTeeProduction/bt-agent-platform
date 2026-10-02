package benchmark

import "testing"

func TestFileTaskRequiresDedicatedBenchmarkFixture(t *testing.T) {
	if err := benchmarkAdmission("FileTask", "personal-report"); err == nil {
		t.Fatal("generic live benchmark admitted filesystem effects without a fixture")
	}
}
