package a2a

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	a2atypes "github.com/a2aproject/a2a-go/v2/a2a"
)

func TestReviewGlobalDiscoveryAdvertisesNoUnservedRPC(t *testing.T) {
	s := &Server{BaseURL: "http://localhost:8686"}
	w := httptest.NewRecorder()
	s.handleGlobalAgentCard(w, httptest.NewRequest(http.MethodGet, "/.well-known/agent-card.json", nil))
	var card a2atypes.AgentCard
	if err := json.Unmarshal(w.Body.Bytes(), &card); err != nil {
		t.Fatal(err)
	}
	if len(card.SupportedInterfaces) != 0 {
		t.Fatalf("aggregate directory advertises RPC interfaces: %+v", card.SupportedInterfaces)
	}
}
func TestReviewA2AHandlerBoundsChunkedBody(t *testing.T) {
	s := &Server{}
	r := httptest.NewRequest(http.MethodGet, "/health", strings.NewReader(strings.Repeat("x", (1<<20)+1)))
	r.ContentLength = -1
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("unbounded A2A handler returned %d", w.Code)
	}
}
