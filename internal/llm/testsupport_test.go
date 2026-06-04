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
	os.Setenv(EnvSkipLLMTests, "1")
	t.Cleanup(func() { os.Unsetenv(EnvSkipLLMTests) })
	if !TestsDisabled() {
		t.Fatal("expected true when BT_SKIP_LLM_TESTS=1")
	}
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
	os.Setenv(EnvSkipLLMTests, "1")
	t.Cleanup(func() {
		os.Unsetenv(EnvSkipLLMTests)
		configuredOnce = sync.Once{}
		configuredVal = false
	})
	SkipIfUnavailable(t)
	t.Fatal("test should have been skipped")
}
