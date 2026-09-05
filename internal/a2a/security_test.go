package a2a

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/nico/go-bt-evolve/internal/agent"
)

func TestSecurityA2AAuthentication(t *testing.T) {
	reg, err := agent.NewRegistry(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reg.Create(agent.Definition{Name: "safe-agent", Tree: "unused"}); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, key, provided string
		want                int
	}{
		{"unconfigured", "", "", 401}, {"unconfigured supplied", "", "fixture-key", 401},
		{"missing", "fixture-key", "", 401}, {"invalid", "fixture-key", "wrong", 401}, {"valid", "fixture-key", "fixture-key", 204},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv, err := NewServer(reg, nil, 0, "http://localhost:0")
			if err != nil {
				t.Fatal(err)
			}
			srv.APIKey = tc.key
			calls := 0
			srv.rpcHandler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Context().Value(agentNameKey{}) != "safe-agent" {
					t.Error("agent context lost")
				}
				w.WriteHeader(http.StatusNoContent)
			})
			req := httptest.NewRequest(http.MethodPost, "/agents/safe-agent", strings.NewReader(`{"jsonrpc":"2.0","method":"SendMessage","id":1}`))
			req.Header.Set("X-API-Key", tc.provided)
			w := httptest.NewRecorder()
			srv.Handler().ServeHTTP(w, req)
			if w.Code != tc.want {
				t.Errorf("status=%d want=%d", w.Code, tc.want)
			}
			if (calls > 0) != (tc.want == 204) {
				t.Errorf("SDK dispatch count=%d", calls)
			}
		})
	}
	// Exercise the actual SDK handler using an invalid method: never executes a tree.
	srv, err := NewServer(reg, nil, 0, "http://localhost:0")
	if err != nil {
		t.Fatal(err)
	}
	srv.APIKey = "fixture-key"
	req := httptest.NewRequest(http.MethodPost, "/agents/safe-agent", strings.NewReader(`{"jsonrpc":"2.0","method":"UnknownSecurityTestMethod","id":1}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", "fixture-key")
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	if !strings.Contains(w.Body.String(), "-32601") {
		t.Fatalf("expected SDK method-not-found: %d %s", w.Code, w.Body.String())
	}
	for _, handler := range []http.HandlerFunc{srv.handleGlobalAgentCard, srv.handleHealth} {
		w := httptest.NewRecorder()
		handler(w, httptest.NewRequest(http.MethodGet, "/", nil))
		if w.Code != 200 {
			t.Errorf("public discovery/health status %d", w.Code)
		}
	}
}

func TestSecurityA2AClientCredentials(t *testing.T) {
	var baseURL string
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/agent-card.json", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-API-Key") != "" {
			t.Error("credential sent to public discovery")
		}
		_ = json.NewEncoder(w).Encode(&a2a.AgentCard{Name: "fixture", SupportedInterfaces: []*a2a.AgentInterface{a2a.NewAgentInterface(baseURL+"/rpc", a2a.TransportProtocolJSONRPC)}})
	})
	mux.HandleFunc("/rpc", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-API-Key") != "fixture-key" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		var req struct {
			ID string `json:"id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Error(err)
		}
		result := a2a.StreamResponse{Event: a2a.NewMessage(a2a.MessageRoleAgent, a2a.NewTextPart("authenticated"))}
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": result})
	})
	ts := httptest.NewServer(mux)
	defer ts.Close()
	baseURL = ts.URL
	c := NewBTAgentClient()
	c.APIKey = "fixture-key"
	c.PlatformURL = ts.URL
	got, err := c.SendTask(context.Background(), ts.URL, "fixture-only")
	if err != nil || got != "authenticated" {
		t.Fatalf("authenticated client: %q %v", got, err)
	}
}

func TestSecurityA2AClientRealAgentEndpoint(t *testing.T) {
	reg, err := agent.NewRegistry(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reg.Create(agent.Definition{Name: "fixture-agent", Tree: "unused"}); err != nil {
		t.Fatal(err)
	}
	srv, err := NewServer(reg, nil, 0, "http://localhost:0")
	if err != nil {
		t.Fatal(err)
	}
	srv.APIKey = "fixture-key"
	// Only SDK dispatch is replaced. Discovery, path routing and auth are real.
	srv.rpcHandler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ID string `json:"id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Error(err)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": a2a.StreamResponse{Event: a2a.NewMessage(a2a.MessageRoleAgent, a2a.NewTextPart("safe reply"))}})
	})
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()
	srv.BaseURL = ts.URL
	if err := srv.RefreshCards(); err != nil {
		t.Fatal(err)
	}
	old := platformClientCredentials.Load()
	t.Cleanup(func() { platformClientCredentials.Store(old) })
	ConfigurePlatformClient("fixture-key", ts.URL)
	c := NewBTAgentClient()
	reply, err := c.SendTask(context.Background(), ts.URL+"/agents/fixture-agent", "fixture")
	if err != nil || reply != "safe reply" {
		t.Fatalf("built-in client against real endpoint: %q %v", reply, err)
	}
}

func TestSecurityA2ACredentialConfinement(t *testing.T) {
	received := make(chan string, 3)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received <- r.Header.Get("X-API-Key")
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	for _, tc := range []struct{ source, platform, want string }{
		{server.URL, server.URL, "fixture-key"},
		{"http://untrusted.invalid", server.URL, ""},
		{server.URL, "http://different-platform.invalid", ""},
	} {
		client := &http.Client{Transport: &platformKeyTransport{key: "fixture-key", sourceURL: tc.source, platformURL: tc.platform}}
		resp, err := client.Get(server.URL)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if got := <-received; got != tc.want {
			t.Errorf("source=%s platform=%s received credential=%t", tc.source, tc.platform, got != "")
		}
	}
}

func TestSecurityA2ARejectsRPCRedirect(t *testing.T) {
	destinationCalls := make(chan struct{}, 10)
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		destinationCalls <- struct{}{}
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer destination.Close()
	var sourceURL string
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/.well-known/agent-card.json" {
			_ = json.NewEncoder(w).Encode(&a2a.AgentCard{Name: "fixture", SupportedInterfaces: []*a2a.AgentInterface{a2a.NewAgentInterface(sourceURL+"/rpc", a2a.TransportProtocolJSONRPC)}})
			return
		}
		http.Redirect(w, r, destination.URL, http.StatusTemporaryRedirect)
	}))
	defer source.Close()
	sourceURL = source.URL
	c := NewBTAgentClient()
	c.APIKey = "fixture-key"
	c.PlatformURL = source.URL
	if _, err := c.SendTask(context.Background(), source.URL, "fixture"); err == nil {
		t.Error("accepted redirected RPC")
	}
	if len(destinationCalls) != 0 {
		t.Fatal("RPC redirect was followed")
	}
}

func TestSecurityA2ARemoteBindWithoutCredentials(t *testing.T) {
	srv := &Server{BindHost: "0.0.0.0", Port: -1}
	if err := srv.Start(); err == nil || !strings.Contains(err.Error(), "API key") {
		t.Fatalf("remote bind did not reject missing credentials before listen: %v", err)
	}
	if srv.httpSrv != nil {
		t.Fatal("HTTP server created without remote-bind credentials")
	}
	srv = &Server{Port: -1}
	if err := srv.Start(); err == nil {
		t.Fatal("invalid port accepted")
	}
	if srv.httpSrv.Addr != "127.0.0.1:-1" {
		t.Errorf("default server bind=%q", srv.httpSrv.Addr)
	}
}
