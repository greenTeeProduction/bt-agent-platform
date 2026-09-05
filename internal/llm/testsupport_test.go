package llm

import (
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
)

func TestTestsDisabled(t *testing.T) {
	os.Unsetenv(EnvSkipLLMTests)
	if TestsDisabled() {
		t.Fatal("expected false when env unset")
	}
	t.Setenv(EnvSkipLLMTests, "1")
	t.Cleanup(func() { os.Unsetenv(EnvSkipLLMTests) })
	if !TestsDisabled() {
		t.Fatal("expected true when BT_SKIP_LLM_TESTS=1")
	}
}

func TestIntegrationOptedIn(t *testing.T) {
	os.Unsetenv(EnvRunLLMTests)
	if IntegrationOptedIn() {
		t.Fatal("expected false when env unset")
	}
	t.Cleanup(func() { os.Unsetenv(EnvRunLLMTests) })
	for _, v := range []string{"1", "true", "YES"} {
		t.Setenv(EnvRunLLMTests, v)
		if !IntegrationOptedIn() {
			t.Fatalf("expected true when BT_RUN_LLM_TESTS=%q", v)
		}
	}
	t.Setenv(EnvRunLLMTests, "0")
	if IntegrationOptedIn() {
		t.Fatal("expected false when BT_RUN_LLM_TESTS=0")
	}
}

func TestSkipUnlessIntegration_SkipsWithoutOptIn(t *testing.T) {
	os.Unsetenv(EnvRunLLMTests)
	t.Cleanup(func() { os.Unsetenv(EnvRunLLMTests) })
	// Without opt-in, this must skip rather than attempt a real LLM call.
	SkipUnlessIntegration(t)
	t.Fatal("test should have been skipped without BT_RUN_LLM_TESTS")
}

func TestOllamaReachable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/tags" {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	if !OllamaReachable(Config{ServerURL: srv.URL}) {
		t.Fatal("expected reachable mock server")
	}
	if OllamaReachable(Config{ServerURL: "http://127.0.0.1:1"}) {
		t.Fatal("expected closed port to be unreachable")
	}
}

func TestSkipIfUnavailable(t *testing.T) {
	t.Setenv(EnvSkipLLMTests, "1")
	t.Cleanup(func() {
		os.Unsetenv(EnvSkipLLMTests)
		configuredOnce = sync.OnceValue(configured)
	})
	SkipIfUnavailable(t)
	t.Fatal("test should have been skipped")
}
