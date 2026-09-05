package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nico/go-bt-evolve/internal/dashboard"
	"github.com/nico/go-bt-evolve/internal/security"
)

func TestReviewPrivilegedDiagnosticsAuthenticated(t *testing.T) {
	old := sessionStore
	sessionStore = security.NewSessionStore(security.SessionStoreConfig{})
	defer func() { sessionStore.Stop(); sessionStore = old }()
	for _, path := range []string{"/api/security/audit", "/api/config", "/api/alerts/rules"} {
		w := httptest.NewRecorder()
		func() {
			defer func() {
				if p := recover(); p != nil {
					t.Errorf("unauthenticated %s reached handler: %v", path, p)
				}
			}()
			dashboardMux("test-key").ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		}()
		if w.Code != 401 {
			t.Errorf("%s code=%d", path, w.Code)
		}
	}
}
func TestReviewSafeMethodsCannotMutate(t *testing.T) {
	old := taskStore
	taskStore = dashboard.NewTaskStore(t.TempDir() + "/tasks.json")
	defer func() { taskStore = old }()
	handlers := []http.HandlerFunc{handleTaskCreate, handleTaskApprove, handleTaskReject, handleWorkflowApprove, handleWorkflowReject, handleAgentRun, handleAnalyze, handleSprintExecute, handleWorkflowRunFullPipeline, handleChat}
	for _, method := range []string{"GET", "HEAD", "OPTIONS"} {
		for i, h := range handlers {
			w := httptest.NewRecorder()
			func() {
				defer func() {
					if p := recover(); p != nil {
						t.Errorf("handler %d reached mutation code: %v", i, p)
					}
				}()
				h(w, httptest.NewRequest(method, "/?title=must-not-create", strings.NewReader(`{}`)))
			}()
			if w.Code != 405 {
				t.Errorf("handler %d %s returned %d want 405", i, method, w.Code)
			}
		}
	}
}
func TestReviewCSRFOnlyAuthenticatedKeyExempt(t *testing.T) {
	old := sessionStore
	sessionStore = security.NewSessionStore(security.SessionStoreConfig{})
	defer func() { sessionStore.Stop(); sessionStore = old }()
	token, err := sessionStore.CreateSession("user")
	if err != nil {
		t.Fatal(err)
	}
	h := dashboardCSRFMiddleware("test-key", dashboardSessionAuth("test-key")(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	for _, tc := range []struct {
		key     string
		session bool
		want    int
	}{{"test-key", false, 204}, {"wrong", false, 403}, {"", true, 403}, {"wrong", true, 403}, {"test-key", true, 204}} {
		r := httptest.NewRequest(http.MethodPost, "/api/agents/execute", strings.NewReader(`{}`))
		r.Header.Set("X-API-Key", tc.key)
		if tc.session {
			r.AddCookie(&http.Cookie{Name: "bt_session", Value: token})
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != tc.want {
			t.Errorf("key=%q session=%v code=%d want=%d", tc.key, tc.session, w.Code, tc.want)
		}
	}
}
