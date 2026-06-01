package security

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestOAuth2IntrospectionValidator_ActiveToken(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request format
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.Header.Get("Content-Type") != "application/x-www-form-urlencoded" {
			t.Errorf("expected application/x-www-form-urlencoded, got %s", r.Header.Get("Content-Type"))
		}
		json.NewEncoder(w).Encode(map[string]interface{}{
			"active": true,
			"sub":    "user-123",
		})
	}))
	defer ts.Close()

	v := OAuth2IntrospectionValidator(OAuth2IntrospectionConfig{
		IntrospectionURL: ts.URL,
		ClientID:         "test-client",
		ClientSecret:     "test-secret",
		CacheTTL:         100 * time.Millisecond,
	})

	subject, err := v(context.Background(), "valid-token")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if subject != "user-123" {
		t.Errorf("expected subject 'user-123', got %q", subject)
	}
}

func TestOAuth2IntrospectionValidator_InactiveToken(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"active": false,
		})
	}))
	defer ts.Close()

	v := OAuth2IntrospectionValidator(OAuth2IntrospectionConfig{
		IntrospectionURL: ts.URL,
		ClientID:         "test-client",
		ClientSecret:     "test-secret",
	})

	_, err := v(context.Background(), "revoked-token")
	if err == nil {
		t.Fatal("expected error for inactive token")
	}
	if !strings.Contains(err.Error(), "not active") {
		t.Errorf("expected 'not active' in error, got: %v", err)
	}
}

func TestOAuth2IntrospectionValidator_ServerError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer ts.Close()

	v := OAuth2IntrospectionValidator(OAuth2IntrospectionConfig{
		IntrospectionURL: ts.URL,
		ClientID:         "test-client",
		ClientSecret:     "test-secret",
	})

	// Server returns 500 with no JSON body — json.Decode fails
	_, err := v(context.Background(), "any-token")
	if err == nil {
		t.Fatal("expected error for server error")
	}
}

func TestOAuth2IntrospectionValidator_CacheHit(t *testing.T) {
	callCount := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		json.NewEncoder(w).Encode(map[string]interface{}{
			"active": true,
			"sub":    "cached-user",
		})
	}))
	defer ts.Close()

	v := OAuth2IntrospectionValidator(OAuth2IntrospectionConfig{
		IntrospectionURL: ts.URL,
		ClientID:         "test-client",
		ClientSecret:     "test-secret",
		CacheTTL:         5 * time.Second, // long enough for test
	})

	// First call — hits server
	subject, err := v(context.Background(), "cached-token")
	if err != nil {
		t.Fatalf("first call: %v", err)
	}
	if subject != "cached-user" {
		t.Errorf("expected 'cached-user', got %q", subject)
	}
	if callCount != 1 {
		t.Errorf("expected 1 server call, got %d", callCount)
	}

	// Second call — should hit cache
	subject, err = v(context.Background(), "cached-token")
	if err != nil {
		t.Fatalf("second call: %v", err)
	}
	if subject != "cached-user" {
		t.Errorf("expected 'cached-user', got %q", subject)
	}
	if callCount != 1 {
		t.Errorf("expected 1 server call (cache hit), got %d", callCount)
	}
}

func TestOAuth2IntrospectionValidator_CacheExpiry(t *testing.T) {
	callCount := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		json.NewEncoder(w).Encode(map[string]interface{}{
			"active": true,
			"sub":    "expiring-user",
		})
	}))
	defer ts.Close()

	v := OAuth2IntrospectionValidator(OAuth2IntrospectionConfig{
		IntrospectionURL: ts.URL,
		ClientID:         "test-client",
		ClientSecret:     "test-secret",
		CacheTTL:         10 * time.Millisecond, // short TTL
	})

	// First call
	_, err := v(context.Background(), "exp-token")
	if err != nil {
		t.Fatalf("first call: %v", err)
	}

	// Wait for cache to expire
	time.Sleep(20 * time.Millisecond)

	// Second call — cache expired, should hit server again
	_, err = v(context.Background(), "exp-token")
	if err != nil {
		t.Fatalf("second call: %v", err)
	}
	if callCount != 2 {
		t.Errorf("expected 2 server calls (cache expired), got %d", callCount)
	}
}

func TestOAuth2IntrospectionValidator_InactiveNotCached(t *testing.T) {
	callCount := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		json.NewEncoder(w).Encode(map[string]interface{}{
			"active": false,
		})
	}))
	defer ts.Close()

	v := OAuth2IntrospectionValidator(OAuth2IntrospectionConfig{
		IntrospectionURL: ts.URL,
		ClientID:         "test-client",
		ClientSecret:     "test-secret",
		CacheTTL:         5 * time.Second,
	})

	// Inactive tokens should NOT be cached — each call hits the server
	_, _ = v(context.Background(), "bad-token")
	_, _ = v(context.Background(), "bad-token")
	if callCount != 2 {
		t.Errorf("expected 2 server calls (inactive not cached), got %d", callCount)
	}
}

func TestOAuth2IntrospectionValidator_SubjectFallback(t *testing.T) {
	tests := []struct {
		name     string
		response map[string]interface{}
		want     string
	}{
		{
			name:     "sub field",
			response: map[string]interface{}{"active": true, "sub": "primary-sub"},
			want:     "primary-sub",
		},
		{
			name:     "username fallback",
			response: map[string]interface{}{"active": true, "username": "fallback-user"},
			want:     "fallback-user",
		},
		{
			name:     "client_id fallback",
			response: map[string]interface{}{"active": true, "client_id": "fallback-client"},
			want:     "fallback-client",
		},
		{
			name:     "no subject fields",
			response: map[string]interface{}{"active": true},
			want:     "oauth2-user",
		},
		{
			name:     "sub takes priority over username",
			response: map[string]interface{}{"active": true, "sub": "primary", "username": "secondary"},
			want:     "primary",
		},
		{
			name:     "username takes priority over client_id",
			response: map[string]interface{}{"active": true, "username": "user", "client_id": "client"},
			want:     "user",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				json.NewEncoder(w).Encode(tt.response)
			}))
			defer ts.Close()

			v := OAuth2IntrospectionValidator(OAuth2IntrospectionConfig{
				IntrospectionURL: ts.URL,
				ClientID:         "test-client",
				ClientSecret:     "test-secret",
				CacheTTL:         1 * time.Millisecond, // don't cache across test cases
			})

			subject, err := v(context.Background(), "token-"+tt.name)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if subject != tt.want {
				t.Errorf("expected subject %q, got %q", tt.want, subject)
			}
		})
	}
}

func TestOAuth2IntrospectionValidator_BasicAuth(t *testing.T) {
	var capturedAuth string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedAuth = r.Header.Get("Authorization")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"active": true,
			"sub":    "auth-test-user",
		})
	}))
	defer ts.Close()

	v := OAuth2IntrospectionValidator(OAuth2IntrospectionConfig{
		IntrospectionURL: ts.URL,
		ClientID:         "my-client",
		ClientSecret:     "my-secret",
	})

	_, err := v(context.Background(), "tok")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expectedAuth := "Basic " + base64.StdEncoding.EncodeToString([]byte(
		url.QueryEscape("my-client")+":"+url.QueryEscape("my-secret"),
	))
	if capturedAuth != expectedAuth {
		t.Errorf("expected auth %q, got %q", expectedAuth, capturedAuth)
	}
}

func TestOAuth2IntrospectionValidator_CustomHTTPClient(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"active": true,
			"sub":    "custom-client-user",
		})
	}))
	defer ts.Close()

	customClient := &http.Client{Timeout: 10 * time.Second}

	v := OAuth2IntrospectionValidator(OAuth2IntrospectionConfig{
		IntrospectionURL: ts.URL,
		ClientID:         "test-client",
		ClientSecret:     "test-secret",
		HTTPClient:       customClient,
	})

	subject, err := v(context.Background(), "tok")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if subject != "custom-client-user" {
		t.Errorf("expected 'custom-client-user', got %q", subject)
	}
}

func TestOAuth2IntrospectionValidator_ContextCancellation(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(100 * time.Millisecond) // slow response
		json.NewEncoder(w).Encode(map[string]interface{}{
			"active": true,
			"sub":    "slow-user",
		})
	}))
	defer ts.Close()

	v := OAuth2IntrospectionValidator(OAuth2IntrospectionConfig{
		IntrospectionURL: ts.URL,
		ClientID:         "test-client",
		ClientSecret:     "test-secret",
		Timeout:          5 * time.Second,
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	_, err := v(ctx, "tok")
	if err == nil {
		t.Fatal("expected error for cancelled context")
	}
}

func TestOAuth2IntrospectionValidator_DefaultConfig(t *testing.T) {
	cfg := DefaultOAuth2IntrospectionConfig("https://auth.example.com/introspect", "cid", "csecret")
	if cfg.CacheTTL != 5*time.Minute {
		t.Errorf("expected CacheTTL 5m, got %v", cfg.CacheTTL)
	}
	if cfg.Timeout != 5*time.Second {
		t.Errorf("expected Timeout 5s, got %v", cfg.Timeout)
	}
	if cfg.IntrospectionURL != "https://auth.example.com/introspect" {
		t.Errorf("expected IntrospectionURL to be set")
	}
}

func TestOAuth2IntrospectionValidator_NilHTTPClient(t *testing.T) {
	// Should use default client when HTTPClient is nil
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"active": true,
			"sub":    "default-client-user",
		})
	}))
	defer ts.Close()

	v := OAuth2IntrospectionValidator(OAuth2IntrospectionConfig{
		IntrospectionURL: ts.URL,
		ClientID:         "test-client",
		ClientSecret:     "test-secret",
		HTTPClient:       nil, // explicitly nil
	})

	subject, err := v(context.Background(), "tok")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if subject != "default-client-user" {
		t.Errorf("expected 'default-client-user', got %q", subject)
	}
}

func TestOAuth2IntrospectionValidator_InvalidJSONResponse(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("not json"))
	}))
	defer ts.Close()

	v := OAuth2IntrospectionValidator(OAuth2IntrospectionConfig{
		IntrospectionURL: ts.URL,
		ClientID:         "test-client",
		ClientSecret:     "test-secret",
	})

	_, err := v(context.Background(), "tok")
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestOAuth2IntrospectionValidator_UnreachableServer(t *testing.T) {
	v := OAuth2IntrospectionValidator(OAuth2IntrospectionConfig{
		IntrospectionURL: "http://127.0.0.1:1/introspect", // nothing listening
		ClientID:         "test-client",
		ClientSecret:     "test-secret",
		Timeout:          50 * time.Millisecond,
	})

	_, err := v(context.Background(), "tok")
	if err == nil {
		t.Fatal("expected error for unreachable server")
	}
}

func TestOAuth2IntrospectionValidator_ZeroCacheTTLDefaults(t *testing.T) {
	callCount := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		json.NewEncoder(w).Encode(map[string]interface{}{
			"active": true,
			"sub":    "zero-ttl-user",
		})
	}))
	defer ts.Close()

	v := OAuth2IntrospectionValidator(OAuth2IntrospectionConfig{
		IntrospectionURL: ts.URL,
		ClientID:         "test-client",
		ClientSecret:     "test-secret",
		CacheTTL:         0, // should default to 5 min
	})

	// Two calls with same token should cache
	_, _ = v(context.Background(), "ztok")
	_, _ = v(context.Background(), "ztok")
	if callCount != 1 {
		t.Errorf("expected 1 server call (cached with default TTL), got %d", callCount)
	}
}

func TestOAuth2IntrospectionValidator_ZeroTimeoutDefaults(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"active": true,
			"sub":    "zero-timeout-user",
		})
	}))
	defer ts.Close()

	v := OAuth2IntrospectionValidator(OAuth2IntrospectionConfig{
		IntrospectionURL: ts.URL,
		ClientID:         "test-client",
		ClientSecret:     "test-secret",
		Timeout:          0, // should default to 5s
	})

	_, err := v(context.Background(), "tok")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestOAuth2IntrospectionValidator_ConcurrentAccess(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"active": true,
			"sub":    "concurrent-user",
		})
	}))
	defer ts.Close()

	v := OAuth2IntrospectionValidator(OAuth2IntrospectionConfig{
		IntrospectionURL: ts.URL,
		ClientID:         "test-client",
		ClientSecret:     "test-secret",
		CacheTTL:         5 * time.Second,
	})

	// Run 50 concurrent validations
	done := make(chan bool)
	for i := 0; i < 50; i++ {
		go func() {
			_, err := v(context.Background(), "shared-token")
			if err != nil {
				t.Errorf("concurrent validation error: %v", err)
			}
			done <- true
		}()
	}

	for i := 0; i < 50; i++ {
		<-done
	}
}

func TestTokenValidator_TypeCompatibility(t *testing.T) {
	// Verify OAuth2IntrospectionValidator returns a TokenValidator
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{"active": true, "sub": "test"})
	}))
	defer ts.Close()

	var v TokenValidator = OAuth2IntrospectionValidator(OAuth2IntrospectionConfig{
		IntrospectionURL: ts.URL,
		ClientID:         "test",
		ClientSecret:     "test",
	})
	_ = v // use the variable
}

func TestOAuth2IntrospectionValidator_DifferentTokensSeparateCache(t *testing.T) {
	callCount := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		// Parse the token from the body
		json.NewEncoder(w).Encode(map[string]interface{}{
			"active": true,
			"sub":    "multi-token-user",
		})
	}))
	defer ts.Close()

	v := OAuth2IntrospectionValidator(OAuth2IntrospectionConfig{
		IntrospectionURL: ts.URL,
		ClientID:         "test-client",
		ClientSecret:     "test-secret",
		CacheTTL:         5 * time.Second,
	})

	// Three different tokens
	_, _ = v(context.Background(), "token-a")
	_, _ = v(context.Background(), "token-b")
	_, _ = v(context.Background(), "token-c")

	// Each unique token triggers a server call
	if callCount != 3 {
		t.Errorf("expected 3 server calls for 3 unique tokens, got %d", callCount)
	}

	// Repeating should use cache
	_, _ = v(context.Background(), "token-a")
	_, _ = v(context.Background(), "token-b")
	if callCount != 3 {
		t.Errorf("expected still 3 server calls (cached), got %d", callCount)
	}
}
