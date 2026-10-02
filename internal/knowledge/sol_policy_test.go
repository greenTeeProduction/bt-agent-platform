package knowledge

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestSolPolicyAllowsConfiguredExternalEmbeddings(t *testing.T) {
	t.Setenv("BT_LLM_SOL_ONLY", "true")
	var calls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		var request struct {
			Model  string `json:"model"`
			Prompt string `json:"prompt"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
		}
		if request.Model != "configured-embedding-model" || request.Prompt != "fixture" {
			t.Errorf("configuration changed: %+v", request)
		}
		_, _ = w.Write([]byte(`{"embedding":[0.1,0.2,0.3]}`))
	}))
	defer server.Close()
	client := &EmbeddingClient{BaseURL: server.URL, Model: "configured-embedding-model"}
	embedding, err := client.GetEmbedding("fixture")
	if err != nil || len(embedding) != 3 || calls.Load() != 1 {
		t.Fatalf("embedding exception failed: %v %v calls=%d", embedding, err, calls.Load())
	}
}
