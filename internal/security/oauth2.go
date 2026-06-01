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

// OAuth2DiscoveryConfig configures OpenID Connect discovery for OAuth2 token introspection.
//
// IssuerURL points at the OAuth2/OIDC issuer base URL, for example
// https://auth.example.com/realms/prod. The discovery request is made to
// {IssuerURL}/.well-known/openid-configuration and must return an
// introspection_endpoint field.
type OAuth2DiscoveryConfig struct {
	// IssuerURL is the OAuth2/OIDC issuer base URL.
	IssuerURL string

	// ClientID and ClientSecret are copied into the resulting introspection config.
	ClientID     string
	ClientSecret string

	// CacheTTL controls the token introspection cache TTL. Default: 5 minutes.
	CacheTTL time.Duration

	// Timeout for the discovery HTTP request and subsequent introspection requests. Default: 5 seconds.
	Timeout time.Duration

	// HTTPClient is an optional pre-configured HTTP client. If nil, a timeout-bound client is created.
	HTTPClient *http.Client
}

// DefaultOAuth2DiscoveryConfig returns an OIDC discovery config with safe defaults.
func DefaultOAuth2DiscoveryConfig(issuerURL, clientID, clientSecret string) OAuth2DiscoveryConfig {
	return OAuth2DiscoveryConfig{
		IssuerURL:    issuerURL,
		ClientID:     clientID,
		ClientSecret: clientSecret,
		CacheTTL:     5 * time.Minute,
		Timeout:      5 * time.Second,
	}
}

// DiscoverOAuth2IntrospectionConfig resolves an RFC 8414/OIDC discovery document
// into an OAuth2IntrospectionConfig. This lets deployments configure only an
// issuer URL instead of hardcoding provider-specific introspection endpoints.
func DiscoverOAuth2IntrospectionConfig(ctx context.Context, cfg OAuth2DiscoveryConfig) (OAuth2IntrospectionConfig, error) {
	if cfg.Timeout <= 0 {
		cfg.Timeout = 5 * time.Second
	}
	if cfg.CacheTTL <= 0 {
		cfg.CacheTTL = 5 * time.Minute
	}
	issuer := strings.TrimRight(strings.TrimSpace(cfg.IssuerURL), "/")
	if issuer == "" {
		return OAuth2IntrospectionConfig{}, fmt.Errorf("oauth2 discovery: issuer URL is required")
	}
	issuerURL, err := url.Parse(issuer)
	if err != nil || issuerURL.Scheme == "" || issuerURL.Host == "" {
		return OAuth2IntrospectionConfig{}, fmt.Errorf("oauth2 discovery: invalid issuer URL %q", cfg.IssuerURL)
	}

	discoveryURL := issuer + "/.well-known/openid-configuration"
	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: cfg.Timeout}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, discoveryURL, nil)
	if err != nil {
		return OAuth2IntrospectionConfig{}, fmt.Errorf("oauth2 discovery: failed to create request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return OAuth2IntrospectionConfig{}, fmt.Errorf("oauth2 discovery: request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return OAuth2IntrospectionConfig{}, fmt.Errorf("oauth2 discovery: unexpected status %d", resp.StatusCode)
	}

	var doc struct {
		Issuer                string `json:"issuer"`
		IntrospectionEndpoint string `json:"introspection_endpoint"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&doc); err != nil {
		return OAuth2IntrospectionConfig{}, fmt.Errorf("oauth2 discovery: failed to parse response: %w", err)
	}
	if strings.TrimSpace(doc.IntrospectionEndpoint) == "" {
		return OAuth2IntrospectionConfig{}, fmt.Errorf("oauth2 discovery: introspection_endpoint missing")
	}
	endpointURL, err := url.Parse(doc.IntrospectionEndpoint)
	if err != nil || endpointURL.Scheme == "" || endpointURL.Host == "" {
		return OAuth2IntrospectionConfig{}, fmt.Errorf("oauth2 discovery: invalid introspection_endpoint %q", doc.IntrospectionEndpoint)
	}

	return OAuth2IntrospectionConfig{
		IntrospectionURL: doc.IntrospectionEndpoint,
		ClientID:         cfg.ClientID,
		ClientSecret:     cfg.ClientSecret,
		CacheTTL:         cfg.CacheTTL,
		Timeout:          cfg.Timeout,
		HTTPClient:       cfg.HTTPClient,
	}, nil
}

// OAuth2DiscoveryValidator discovers the provider introspection endpoint and
// returns a TokenValidator backed by OAuth2IntrospectionValidator.
func OAuth2DiscoveryValidator(ctx context.Context, cfg OAuth2DiscoveryConfig) (TokenValidator, error) {
	introspectionCfg, err := DiscoverOAuth2IntrospectionConfig(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return OAuth2IntrospectionValidator(introspectionCfg), nil
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
