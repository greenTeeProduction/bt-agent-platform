package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestOpenAICompat_GenerateWithModel_SendsChatCompletion(t *testing.T) {
	var gotModel string
	var gotAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		var req openAICompatRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		gotModel = req.Model
		if len(req.Messages) != 2 || req.Messages[0].Role != "system" || req.Messages[1].Role != "user" {
			t.Fatalf("unexpected messages: %#v", req.Messages)
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"panel answer"}}]}`))
	}))
	defer server.Close()

	client := NewOpenAICompatClient(OpenAICompatConfig{APIKey: "key", BaseURL: server.URL, Model: "default", Timeout: time.Second})
	got, err := client.GenerateWithModel(context.Background(), "model-a", "sys", "prompt")
	if err != nil {
		t.Fatalf("GenerateWithModel error: %v", err)
	}
	if got != "panel answer" || gotModel != "model-a" || gotAuth != "Bearer key" {
		t.Fatalf("got=%q model=%q auth=%q", got, gotModel, gotAuth)
	}
}

func TestOpenAICompat_ErrorResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"message":"bad model"}}`))
	}))
	defer server.Close()

	client := NewOpenAICompatClient(OpenAICompatConfig{BaseURL: server.URL, Model: "default", Timeout: time.Second})
	if _, err := client.Generate("prompt"); err == nil {
		t.Fatal("expected error response")
	}
}
