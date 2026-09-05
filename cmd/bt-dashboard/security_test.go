package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nico/go-bt-evolve/internal/config"
	"github.com/nico/go-bt-evolve/internal/security"
)

func TestSecurityDashboardRoutesRequireCredentials(t *testing.T) {
	old := sessionStore
	sessionStore = security.NewSessionStore(security.SessionStoreConfig{})
	t.Cleanup(func() { sessionStore.Stop(); sessionStore = old })
	t.Setenv("BT_AGENT_HOME", t.TempDir())
	for _, key := range []string{"", "fixture-key"} {
		for _, provided := range []string{"", "wrong", "fixture-key"} {
			mux := dashboardMux(key)
			for _, path := range []string{"/api/agents/create", "/api/agents/delete", "/api/agents/run", "/api/agents/execute", "/api/tasks/approve", "/api/pipelines/run", "/api/blackboard"} {
				// Invalid JSON ensures a regression cannot actually create or execute work.
				w := httptest.NewRecorder()
				req := httptest.NewRequest(http.MethodPost, path, strings.NewReader("{"))
				req.Header.Set("X-API-Key", provided)
				mux.ServeHTTP(w, req)
				if key == "" || key != provided {
					if w.Code != 401 {
						t.Errorf("key configured=%t supplied=%q path=%s status=%d want=401", key != "", provided, path, w.Code)
					}
				} else if w.Code == 401 {
					t.Errorf("configured key rejected at %s", path)
				}
			}
		}
	}
}

func TestSecurityDashboardConfigKeyLogin(t *testing.T) {
	t.Setenv("BT_API_KEY", "")
	oldCfg, oldStore, oldThrottle := dashConfig, sessionStore, loginThrottle
	dashConfig = &config.Config{APIKey: "fixture-config-key"}
	sessionStore = security.NewSessionStore(security.SessionStoreConfig{CookieSecure: true})
	loginThrottle = security.NewLoginThrottle(security.DefaultLoginThrottleConfig())
	t.Cleanup(func() { sessionStore.Stop(); dashConfig, sessionStore, loginThrottle = oldCfg, oldStore, oldThrottle })
	mux := dashboardMux(dashConfig.APIKey)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/login", strings.NewReader(`{"password":"fixture-config-key"}`)))
	if w.Code != 200 {
		t.Fatalf("config-key login status=%d body=%s", w.Code, w.Body.String())
	}
	cookies := w.Result().Cookies()
	if len(cookies) == 0 || !cookies[0].Secure {
		t.Fatal("missing secure session cookie")
	}
	for _, path := range []string{"/api/session", "/api/agents"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.AddCookie(cookies[0])
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)
		if w.Code != 200 {
			t.Errorf("session at %s returned %d", path, w.Code)
		}
	}
	req := httptest.NewRequest(http.MethodGet, "/api/session", nil)
	req.Header.Set("X-API-Key", "fixture-config-key")
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Errorf("config-key session inspection: %d", w.Code)
	}
}
func TestSecurityDashboardRejectsInvalidNames(t *testing.T) {
	t.Setenv("BT_AGENT_HOME", t.TempDir())
	for _, path := range []string{"/api/agents/create", "/api/agents/delete"} {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"name":"../outside","tree":"unused"}`))
		if strings.HasSuffix(path, "create") {
			handleAgentCreate(w, req)
		} else {
			handleAgentDelete(w, req)
		}
		if w.Code != 400 {
			t.Errorf("%s returned %d want 400", path, w.Code)
		}
	}
}

func TestSecurityDashboardEmptyKeyRejectsSession(t *testing.T) {
	old := sessionStore
	sessionStore = security.NewSessionStore(security.SessionStoreConfig{})
	t.Cleanup(func() { sessionStore.Stop(); sessionStore = old })
	token, err := sessionStore.CreateSession("")
	if err != nil {
		t.Fatal(err)
	}
	cookieWriter := httptest.NewRecorder()
	sessionStore.SetSessionCookie(cookieWriter, token)
	req := httptest.NewRequest(http.MethodPost, "/api/agents/create", strings.NewReader("{"))
	req.AddCookie(cookieWriter.Result().Cookies()[0])
	w := httptest.NewRecorder()
	dashboardMux("").ServeHTTP(w, req)
	if w.Code != 401 {
		t.Errorf("unconfigured server accepted session: %d", w.Code)
	}
}
