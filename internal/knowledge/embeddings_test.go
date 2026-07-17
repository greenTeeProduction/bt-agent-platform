package knowledge

import (
	"math"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nico/go-bt-evolve/internal/reliability"
)

// =============================================================================
// CosineSimilarity (pure math, no Ollama needed)
// =============================================================================

func TestCosineSimilarity_Identical(t *testing.T) {
	a := Embedding{1.0, 2.0, 3.0}
	b := Embedding{1.0, 2.0, 3.0}
	sim := CosineSimilarity(a, b)
	if math.Abs(sim-1.0) > 0.0001 {
		t.Errorf("identical vectors should have similarity 1.0, got %.4f", sim)
	}
}

func TestCosineSimilarity_Opposite(t *testing.T) {
	a := Embedding{1.0, 0.0}
	b := Embedding{-1.0, 0.0}
	sim := CosineSimilarity(a, b)
	if math.Abs(sim+1.0) > 0.0001 {
		t.Errorf("opposite vectors should have similarity -1.0, got %.4f", sim)
	}
}

func TestCosineSimilarity_Orthogonal(t *testing.T) {
	a := Embedding{1.0, 0.0}
	b := Embedding{0.0, 1.0}
	sim := CosineSimilarity(a, b)
	if math.Abs(sim-0.0) > 0.0001 {
		t.Errorf("orthogonal vectors should have similarity 0.0, got %.4f", sim)
	}
}

func TestCosineSimilarity_Partial(t *testing.T) {
	a := Embedding{1.0, 2.0, 3.0}
	b := Embedding{4.0, 5.0, 6.0}
	sim := CosineSimilarity(a, b)
	// (4+10+18) / (sqrt(14) * sqrt(77)) = 32 / (3.742 * 8.775) = 32 / 32.831 ≈ 0.9746
	expected := 32.0 / (math.Sqrt(14.0) * math.Sqrt(77.0))
	if math.Abs(sim-expected) > 0.0001 {
		t.Errorf("expected similarity ~%.4f, got %.4f", expected, sim)
	}
}

func TestCosineSimilarity_Empty(t *testing.T) {
	sim := CosineSimilarity(Embedding{}, Embedding{1.0, 2.0})
	if sim != 0.0 {
		t.Errorf("empty vector should return 0.0, got %.2f", sim)
	}
}

func TestCosineSimilarity_Nil(t *testing.T) {
	sim := CosineSimilarity(nil, Embedding{1.0})
	if sim != 0.0 {
		t.Errorf("nil vector should return 0.0, got %.2f", sim)
	}
}

func TestCosineSimilarity_DifferentLengths(t *testing.T) {
	a := Embedding{1.0, 2.0}
	b := Embedding{1.0, 2.0, 3.0}
	sim := CosineSimilarity(a, b)
	if sim != 0.0 {
		t.Errorf("different lengths should return 0.0, got %.2f", sim)
	}
}

func TestCosineSimilarity_ZeroNorm(t *testing.T) {
	a := Embedding{0.0, 0.0}
	b := Embedding{1.0, 0.0}
	sim := CosineSimilarity(a, b)
	if sim != 0.0 {
		t.Errorf("zero-norm vector should return 0.0, got %.2f", sim)
	}
}

func TestCosineSimilarity_BothZero(t *testing.T) {
	a := Embedding{0.0, 0.0}
	b := Embedding{0.0, 0.0}
	sim := CosineSimilarity(a, b)
	if sim != 0.0 {
		t.Errorf("both zero-norm should return 0.0, got %.2f", sim)
	}
}

// =============================================================================
// BuildIndex (per-tree goroutine panic recovery)
// =============================================================================

// TestBuildIndex_PanicRecovered verifies that a panic in a per-tree embedding
// goroutine (e.g. from a nil tree blowing up during capability-text assembly,
// or from a panic inside the embedding client itself) is turned into an error
// result on the goroutine's result channel instead of crashing the process or
// deadlocking the synchronous receive loop that waits for one result per tree.
func TestBuildIndex_PanicRecovered(t *testing.T) {
	kg := NewKnowledgeGraph()
	// A nil TreeMeta panics as soon as BuildIndex's goroutine dereferences it
	// (t.Name, at the start of capability-text assembly) — simulating any panic
	// raised while building the embedding for this tree.
	kg.Trees["panicky"] = nil

	done := make(chan error, 1)
	go func() {
		done <- kg.BuildIndex()
	}()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected BuildIndex to return an error when a per-tree goroutine panics, got nil")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("BuildIndex hung after a per-tree goroutine panic instead of returning an error")
	}
}

// =============================================================================
// GetEmbedding (bounded timeout against an unresponsive backend)
// =============================================================================

// TestGetEmbedding_TimesOutOnUnresponsiveBackend verifies that GetEmbedding
// returns an error within a bounded deadline when the Ollama backend accepts
// the connection but never writes a response, instead of hanging forever the
// way http.Post with http.DefaultClient's zero timeout would.
// TestGetEmbedding_HTTPErrorStatusIsError pins the status-before-parse
// mechanism (mirroring internal/llm/openai_compat.go): an HTTP error response
// whose JSON body decodes cleanly into the embedding struct (Ollama's normal
// {"error":"..."} shape, e.g. model-not-pulled 404) must surface as an error —
// the pre-fix code returned SUCCESS with a nil embedding and recorded a
// breaker success, so a persistently erroring backend never opened the
// breaker and discovery silently degraded.
func TestGetEmbedding_HTTPErrorStatusIsError(t *testing.T) {
	origCB := embeddingBreaker
	t.Cleanup(func() { embeddingBreaker = origCB })
	embeddingBreaker = reliability.NewCircuitBreaker("ollama-embeddings", 3, time.Minute)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":"model \"missing\" not found, try pulling it first"}`))
	}))
	defer srv.Close()

	ec := &EmbeddingClient{BaseURL: srv.URL, Model: "missing"}
	emb, err := ec.GetEmbedding("text")
	if err == nil {
		t.Fatalf("expected an error for HTTP 404, got success with %d-dim embedding", len(emb))
	}
}

// TestGetEmbedding_EmptyEmbeddingOn200IsError: a 2xx whose body carries no
// embedding vector is not a usable success — treating it as one poisons the
// index with nil vectors and silently disables embedding discovery.
func TestGetEmbedding_EmptyEmbeddingOn200IsError(t *testing.T) {
	origCB := embeddingBreaker
	t.Cleanup(func() { embeddingBreaker = origCB })
	embeddingBreaker = reliability.NewCircuitBreaker("ollama-embeddings", 3, time.Minute)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	ec := &EmbeddingClient{BaseURL: srv.URL, Model: "m"}
	if _, err := ec.GetEmbedding("text"); err == nil {
		t.Fatal("expected an error for a 200 response with no embedding vector")
	}
}

// TestGetEmbedding_5xxIsRetriedThenSucceeds: a transient backend 5xx must be
// classified retryable (typed, not dependent on body substrings) so the retry
// policy recovers on the next attempt.
func TestGetEmbedding_5xxIsRetriedThenSucceeds(t *testing.T) {
	origCB := embeddingBreaker
	t.Cleanup(func() { embeddingBreaker = origCB })
	embeddingBreaker = reliability.NewCircuitBreaker("ollama-embeddings", 3, time.Minute)

	var n int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&n, 1) == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte("<html>backend restarting</html>"))
			return
		}
		_, _ = w.Write([]byte(`{"embedding":[0.1,0.2,0.3]}`))
	}))
	defer srv.Close()

	ec := &EmbeddingClient{BaseURL: srv.URL, Model: "m"}
	emb, err := ec.GetEmbedding("text")
	if err != nil {
		t.Fatalf("expected the retry policy to recover from a transient 500, got: %v", err)
	}
	if len(emb) != 3 {
		t.Fatalf("got %d-dim embedding, want 3", len(emb))
	}
	if atomic.LoadInt32(&n) < 2 {
		t.Fatalf("expected at least 2 attempts, got %d", n)
	}
}

func TestGetEmbedding_TimesOutOnUnresponsiveBackend(t *testing.T) {
	block := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-block // never respond
	}))
	// srv.Close() blocks until in-flight handlers return, so unblock the
	// handler (close(block)) before closing the server, not after.
	defer srv.Close()
	defer close(block)

	ec := &EmbeddingClient{BaseURL: srv.URL, Model: "test-model"}

	done := make(chan error, 1)
	go func() {
		_, err := ec.GetEmbedding("test text")
		done <- err
	}()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected GetEmbedding to return an error for an unresponsive backend, got nil")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("GetEmbedding hung past the bounded deadline instead of timing out on an unresponsive backend")
	}
}

// =============================================================================
// Circuit breaker (short-circuit repeated failures instead of retrying a dead
// Ollama endpoint on every call)
// =============================================================================

// withFreshEmbeddingBreaker swaps the package-level embedding circuit breaker
// for a freshly closed one for the duration of the test, restoring the prior
// breaker afterward so this test cannot leak open/tripped state into other
// tests in the package that expect a working embeddings backend.
func withFreshEmbeddingBreaker(t *testing.T, threshold int, cooldown time.Duration) {
	t.Helper()
	orig := embeddingBreaker
	t.Cleanup(func() { embeddingBreaker = orig })
	embeddingBreaker = reliability.NewCircuitBreaker("ollama-embeddings", threshold, cooldown)
}

// TestGetEmbedding_CircuitBreakerOpensAfterConsecutiveFailures verifies that
// after enough consecutive failures the breaker opens and short-circuits
// further calls instead of hitting the dead backend on every single request.
func TestGetEmbedding_CircuitBreakerOpensAfterConsecutiveFailures(t *testing.T) {
	withFreshEmbeddingBreaker(t, 3, time.Minute)

	var requests int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requests, 1)
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()

	ec := &EmbeddingClient{BaseURL: srv.URL, Model: "test-model"}

	// Threshold is 3: the first three calls each fail (a 5xx is retryable, so
	// each call may make several attempts) and open the breaker.
	const calls = 3
	for i := 0; i < calls; i++ {
		if _, err := ec.GetEmbedding("task"); err == nil {
			t.Fatalf("call %d: expected error from failing backend, got nil", i)
		}
	}
	if state := embeddingBreaker.State(); state != reliability.CircuitOpen {
		t.Fatalf("expected circuit breaker to be open after %d consecutive failures, got state %v", calls, state)
	}

	// Once open, further calls must short-circuit without touching the dead
	// backend at all.
	before := atomic.LoadInt32(&requests)
	for i := 0; i < 3; i++ {
		if _, err := ec.GetEmbedding("task"); err == nil {
			t.Fatalf("post-open call %d: expected the breaker-open error, got nil", i)
		}
	}
	if got := atomic.LoadInt32(&requests); got != before {
		t.Errorf("open breaker must short-circuit: backend saw %d extra requests after opening", got-before)
	}
}

// =============================================================================
// Retry (jittered backoff on transient failures)
// =============================================================================

// TestGetEmbedding_RetriesOnceOnTransientFailureThenSucceeds verifies that a
// transient, connection-level failure (e.g. Ollama mid-restart) is retried
// with reliability.DefaultRetryPolicy()'s full-jitter backoff instead of
// failing the call immediately — a client that fails once then succeeds
// should return a valid embedding after exactly one retry.
func TestGetEmbedding_RetriesOnceOnTransientFailureThenSucceeds(t *testing.T) {
	withFreshEmbeddingBreaker(t, 3, time.Minute)

	var attempts int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&attempts, 1) == 1 {
			// Simulate a transient network failure (e.g. Ollama mid-restart)
			// by hijacking and closing the connection with no response, which
			// surfaces to the client as a network-classified error.
			if hj, ok := w.(http.Hijacker); ok {
				if conn, _, err := hj.Hijack(); err == nil {
					conn.Close()
					return
				}
			}
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"embedding":[0.1,0.2,0.3]}`))
	}))
	defer srv.Close()

	ec := &EmbeddingClient{BaseURL: srv.URL, Model: "test-model"}

	emb, err := ec.GetEmbedding("test text")
	if err != nil {
		t.Fatalf("expected GetEmbedding to succeed after one jittered retry on a transient failure, got error: %v", err)
	}
	if len(emb) != 3 {
		t.Fatalf("expected a 3-dim embedding after retry, got %v", emb)
	}
	if got := atomic.LoadInt32(&attempts); got != 2 {
		t.Errorf("expected exactly 2 attempts (1 transient failure + 1 successful retry), got %d", got)
	}
}

// TestDiscoverWithEmbeddings_FallsBackWhenBreakerOpen verifies that once the
// breaker has tripped, discoverWithEmbeddings still returns a clean fallback
// (empty id, zero score) instead of hanging or panicking.
func TestDiscoverWithEmbeddings_FallsBackWhenBreakerOpen(t *testing.T) {
	withFreshEmbeddingBreaker(t, 3, time.Minute)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()

	orig := defaultEmbeddingClient
	defer func() { defaultEmbeddingClient = orig }()
	defaultEmbeddingClient = &EmbeddingClient{BaseURL: srv.URL, Model: "test"}

	kg := NewKnowledgeGraph()
	kg.Register(&TreeMeta{ID: "tree:a", Name: "A", Category: "test", Fitness: 50, Embedding: Embedding{1, 0, 0}})

	// Trip the breaker before exercising discoverWithEmbeddings.
	for i := 0; i < 5; i++ {
		_, _ = defaultEmbeddingClient.GetEmbedding("warm up")
	}
	if state := embeddingBreaker.State(); state != reliability.CircuitOpen {
		t.Fatalf("setup: expected circuit breaker open before exercising fallback, got %v", state)
	}

	done := make(chan struct{})
	var id string
	var score float64
	go func() {
		id, score = kg.discoverWithEmbeddings("some task")
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("discoverWithEmbeddings hung instead of falling back cleanly while the circuit breaker is open")
	}

	if id != "" || score != 0 {
		t.Errorf("expected clean fallback (empty id, zero score) while the embeddings backend is circuit-broken, got id=%q score=%.2f", id, score)
	}
}
