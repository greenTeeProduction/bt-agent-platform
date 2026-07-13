package knowledge

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"time"

	"github.com/nico/go-bt-evolve/internal/reliability"
)

// Embedding is a vector representation of text.
type Embedding []float64

// EmbeddingClient talks to Ollama's embedding API.
type EmbeddingClient struct {
	BaseURL string
	Model   string
}

var defaultEmbeddingClient = &EmbeddingClient{
	BaseURL: "http://localhost:11434",
	Model:   "nomic-embed-text",
}

// embeddingHTTPClient bounds every Ollama embedding request so an
// unresponsive backend fails fast instead of hanging the caller forever
// (http.DefaultClient, used by http.Post, has no timeout).
var embeddingHTTPClient = &http.Client{Timeout: 2 * time.Second}

// embeddingBreaker short-circuits Ollama embedding calls after repeated
// failures so a dead backend isn't retried (and re-timed-out) on every call.
var embeddingBreaker = reliability.NewCircuitBreaker("ollama-embeddings", 3, 60*time.Second)

// GetEmbedding returns the embedding vector for a text.
func (ec *EmbeddingClient) GetEmbedding(text string) (Embedding, error) {
	if !embeddingBreaker.Allow() {
		return nil, fmt.Errorf("ollama embedding: circuit breaker open")
	}

	payload := map[string]interface{}{
		"model":  ec.Model,
		"prompt": text,
	}
	body, _ := json.Marshal(payload)

	resp, err := embeddingHTTPClient.Post(ec.BaseURL+"/api/embeddings", "application/json", bytes.NewReader(body))
	if err != nil {
		embeddingBreaker.RecordFailure()
		return nil, fmt.Errorf("ollama embedding: %w", err)
	}
	defer resp.Body.Close()

	var result struct {
		Embedding []float64 `json:"embedding"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		embeddingBreaker.RecordFailure()
		return nil, fmt.Errorf("decode embedding: %w", err)
	}
	embeddingBreaker.RecordSuccess()
	return Embedding(result.Embedding), nil
}

// CosineSimilarity returns the cosine similarity between two embeddings.
func CosineSimilarity(a, b Embedding) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	var dot, normA, normB float64
	for i := range a {
		dot += a[i] * b[i]
		normA += a[i] * a[i]
		normB += b[i] * b[i]
	}
	if normA == 0 || normB == 0 {
		return 0
	}
	return dot / (math.Sqrt(normA) * math.Sqrt(normB))
}

// BuildIndex generates embeddings for all trees in the graph concurrently.
func (kg *KnowledgeGraph) BuildIndex() error {
	type result struct {
		id  string
		emb Embedding
		err error
	}
	ch := make(chan result, len(kg.Trees))

	for id, tree := range kg.Trees {
		reliability.SafeGo(fmt.Sprintf("knowledge.BuildIndex tree %s", id), func() {
			text := tree.Name + " " + tree.Description
			for _, cap := range tree.Capabilities {
				text += " " + cap.Action + " in " + cap.Domain
			}
			emb, err := defaultEmbeddingClient.GetEmbedding(text)
			kg.mu.Lock()
			if err == nil {
				tree.Embedding = emb
			}
			kg.mu.Unlock()
			ch <- result{id: id, emb: emb, err: err}
		}, func(panicVal any, context string) {
			ch <- result{id: id, err: fmt.Errorf("panic in [%s]: %v", context, panicVal)}
		})
	}

	var firstErr error
	for i := 0; i < len(kg.Trees); i++ {
		r := <-ch
		if r.err != nil && firstErr == nil {
			firstErr = r.err
		}
	}
	return firstErr
}

// hasEmbeddings checks if any trees have embeddings.
func (kg *KnowledgeGraph) hasEmbeddings() bool {
	for _, tree := range kg.Trees {
		if len(tree.Embedding) > 0 {
			return true
		}
	}
	return false
}

// discoverWithEmbeddings finds the best tree using embedding similarity.
func (kg *KnowledgeGraph) discoverWithEmbeddings(task string) (string, float64) {
	taskEmb, err := defaultEmbeddingClient.GetEmbedding(task)
	if err != nil {
		return "", 0
	}

	best := ""
	bestScore := -1.0
	for id, tree := range kg.Trees {
		if len(tree.Embedding) == 0 {
			continue
		}
		sim := CosineSimilarity(taskEmb, tree.Embedding)
		// Boost by fitness (0-100 scaled to 0-1)
		sim = 0.7*sim + 0.3*(tree.Fitness/100.0)
		// Break equal-similarity ties by sorted tree ID so map iteration order
		// can never decide the winner.
		if sim > bestScore || (sim == bestScore && best != "" && id < best) {
			bestScore = sim
			best = id
		}
	}
	if bestScore > 0.4 {
		return best, bestScore
	}
	return "", 0
}
