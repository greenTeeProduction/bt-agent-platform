package main

import (
	"compress/gzip"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/nico/go-bt-evolve/internal/agent"
	"github.com/nico/go-bt-evolve/internal/engine"
	"github.com/nico/go-bt-evolve/internal/evolution"
	"github.com/nico/go-bt-evolve/internal/reliability"
	"github.com/nico/go-bt-evolve/internal/security"
	btcore "github.com/rvitorper/go-bt/core"
)

func TestReviewRemoteExecutorProductionMiddleware(t *testing.T) {
	t.Setenv("BT_AGENT_HOME", t.TempDir())
	t.Setenv("BT_API_KEY", "remote-key")
	old, runner := sessionStore, dashAgentRunner
	sessionStore = security.NewSessionStore(security.SessionStoreConfig{})
	defer func() { sessionStore.Stop(); sessionStore = old; dashAgentRunner = runner }()
	actionName := "review_remote" + t.TempDir()
	engine.RegisterAction(actionName, func(ctx *btcore.BTContext[engine.Blackboard]) int {
		ctx.Blackboard.Result = "status: remote execution completed"
		return 1
	})
	dashAgentRunner = &agent.RunDeps{ResolveTree: func(string) *evolution.SerializableNode {
		return &evolution.SerializableNode{Type: "Action", Name: actionName}
	}}
	h := dashboardMiddleware(dashboardMux("remote-key"), "remote-key", false, "", true, security.NewRateLimiter(1000, 1000))
	srv := httptest.NewServer(h)
	defer srv.Close()
	re := reliability.NewRemoteExecutor(reliability.RemoteExecutorConfig{BaseURL: srv.URL, APIKey: "remote-key"})
	result, err := re.Execute(t.Context(), "review-agent", "review task")
	if err != nil {
		t.Fatal(err)
	}
	if result.Outcome != "success" {
		t.Fatalf("remote result %+v", result)
	}
	r := httptest.NewRequest(http.MethodGet, "/api/session", nil)
	r.Header.Set("X-API-Key", "remote-key")
	r.Header.Set("Accept-Encoding", "gzip")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("valid gzip response rejected: %d %s", w.Code, w.Body.String())
	}
	z, err := gzip.NewReader(w.Body)
	if err != nil {
		t.Fatal(err)
	}
	defer z.Close()
	body, err := io.ReadAll(z)
	if err != nil {
		t.Fatal(err)
	}
	if !json.Valid(body) {
		t.Fatalf("invalid compressed JSON %s", body)
	}
}
