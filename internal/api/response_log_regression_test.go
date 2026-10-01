package api

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestResponseValidatorLogsCatalogIdentityWithoutRequestData(t *testing.T) {
	var logs bytes.Buffer
	route := NewRoute("/api/item/{id}", GET).JSONResponse(200, "fixture", ObjectSchema(map[string]*Schema{"value": IntSchema("value")}, "value")).Build()
	handler := ResponseValidator([]Route{route}, &ResponseValidatorConfig{Logger: slog.New(slog.NewTextHandler(&logs, nil))})(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`{"value":"invalid fixture"}`)) }))
	r := httptest.NewRequest(http.MethodGet, "/api/item/attacker%0Aforged", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "invalid fixture") {
		t.Fatal("observation changed response")
	}
	if strings.Contains(logs.String(), "attacker") || strings.Contains(logs.String(), "forged") || strings.Contains(logs.String(), "invalid fixture") {
		t.Fatalf("request data copied into operator logs: %s", logs.String())
	}
	if !strings.Contains(logs.String(), "/api/item/{id}") || !strings.Contains(logs.String(), "violations=1") {
		t.Fatalf("catalog/count diagnostic missing: %s", logs.String())
	}
}
