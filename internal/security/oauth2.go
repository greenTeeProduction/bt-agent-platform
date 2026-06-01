// Package security provides OAuth2 token validation via RFC 7662 introspection.
//
// OAuth2IntrospectionValidator returns a TokenValidator that validates bearer tokens
// by calling an OAuth2 introspection endpoint. Results are cached with a configurable
// TTL to minimize latency on repeated token validation.
//
// RFC 7662: https://datatracker.ietf.org/doc/html/rfc7662
package security

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// OAuth2IntrospectionConfig configures an OAuth2 token introspection validator.
type OAuth2IntrospectionConfig struct {
	// IntrospectionURL is the RFC 7662 introspection endpoint (e.g. https://auth.example.com/oauth2/introspect).
	IntrospectionURL string

	// ClientID and ClientSecret are used for HTTP Basic authentication with the introspection endpoint.
	ClientID     string
	ClientSecret string

	// CacheTTL controls how long successful introspection results are cached. Default: 5 minutes.
	CacheTTL time.Duration

	// Timeout for introspection HTTP requests. Default: 5 seconds.
	Timeout time.Duration

	// HTTPClient is an optional pre-configured HTTP client. If nil, http.DefaultClient is used.
	HTTPClient *http.Client
}

// DefaultOAuth2IntrospectionConfig returns a Config with safe defaults.
func DefaultOAuth2IntrospectionConfig(introspectionURL, clientID, clientSecret string) OAuth2IntrospectionConfig {
	return OAuth2IntrospectionConfig{
		IntrospectionURL: introspectionURL,
		ClientID:         clientID,
		ClientSecret:     clientSecret,
		CacheTTL:         5 * time.Minute,
		Timeout:          5 * time.Second,
	}
}

// introspectionResult is the cached result of a token introspection call.
type introspectionResult struct {
	Subject string
	Active  bool
	Expires time.Time // cache expiry, not token expiry
}

// OAuth2IntrospectionValidator returns a TokenValidator that validates bearer tokens
// via RFC 7662 token introspection with in-memory caching.
//
// The validator sends POST requests to the configured introspection endpoint with
// the token as a form-encoded body parameter. Authentication uses HTTP Basic auth
// with the client ID and secret. A response with active:true is considered valid.
// The subject is extracted from the "sub" claim, falling back to "username" or "client_id".
//
// Caching: successful validations (active=true) are cached for CacheTTL.
// Failed validations (active=false) and errors are NOT cached, ensuring
// revoked tokens are detected quickly.
func OAuth2IntrospectionValidator(cfg OAuth2IntrospectionConfig) TokenValidator {
	if cfg.CacheTTL <= 0 {
		cfg.CacheTTL = 5 * time.Minute
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 5 * time.Second
	}

	var (
		mu     sync.RWMutex
		cache  = make(map[string]introspectionResult)
		client = cfg.HTTPClient
	)
	if client == nil {
		client = &http.Client{Timeout: cfg.Timeout}
	}

	// Pre-compute the basic auth header value
	authHeader := "Basic " + base64.StdEncoding.EncodeToString([]byte(
		url.QueryEscape(cfg.ClientID)+":"+url.QueryEscape(cfg.ClientSecret),
	))

	return func(ctx context.Context, token string) (string, error) {
		// Check cache
		mu.RLock()
		if cached, ok := cache[token]; ok && time.Now().Before(cached.Expires) {
			mu.RUnlock()
			if !cached.Active {
				return "", fmt.Errorf("token is inactive")
			}
			return cached.Subject, nil
		}
		mu.RUnlock()

		// Build introspection request body (application/x-www-form-urlencoded)
		bodyStr := "token=" + url.QueryEscape(token)
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.IntrospectionURL,
			strings.NewReader(bodyStr))
		if err != nil {
			return "", fmt.Errorf("oauth2 introspection: failed to create request: %w", err)
		}
		req.Header.Set("Authorization", authHeader)
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.ContentLength = int64(len(bodyStr))

		// Execute introspection request
		resp, err := client.Do(req)
		if err != nil {
			return "", fmt.Errorf("oauth2 introspection: request failed: %w", err)
		}
		defer resp.Body.Close()

		// Parse introspection response (RFC 7662)
		var introspect struct {
			Active   bool   `json:"active"`
			Subject  string `json:"sub"`
			Username string `json:"username"`
			ClientID string `json:"client_id"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&introspect); err != nil {
			return "", fmt.Errorf("oauth2 introspection: failed to parse response: %w", err)
		}

		if !introspect.Active {
			// Don't cache inactive tokens — they might become active again,
			// or we want to detect revocation quickly.
			return "", fmt.Errorf("token is not active")
		}

		// Determine subject: prefer "sub", fall back to "username" or "client_id"
		subject := introspect.Subject
		if subject == "" {
			subject = introspect.Username
		}
		if subject == "" {
			subject = introspect.ClientID
		}
		if subject == "" {
			subject = "oauth2-user"
		}

		// Cache successful result
		mu.Lock()
		cache[token] = introspectionResult{
			Subject: subject,
			Active:  true,
			Expires: time.Now().Add(cfg.CacheTTL),
		}
		// Simple cache eviction: keep at most 10000 entries by purging expired
		if len(cache) > 10000 {
			now := time.Now()
			for k, v := range cache {
				if now.After(v.Expires) {
					delete(cache, k)
				}
			}
		}
		mu.Unlock()

		return subject, nil
	}
}
