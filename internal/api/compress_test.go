package api

import (
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// gzipDecompress helper for reading compressed response bodies in tests.
func gzipDecompress(t *testing.T, r io.Reader) string {
	t.Helper()
	gz, err := gzip.NewReader(r)
	if err != nil {
		t.Fatalf("gzip.NewReader: %v", err)
	}
	defer gz.Close()
	data, err := io.ReadAll(gz)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	return string(data)
}

func TestCompressionMiddleware_GzipRequest(t *testing.T) {
	handler := CompressionMiddleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"message":"hello world","count":42}`))
	}))

	req := httptest.NewRequest("GET", "/api/test", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}

	ce := rec.Header().Get("Content-Encoding")
	if ce != "gzip" {
		t.Errorf("Content-Encoding = %q, want %q", ce, "gzip")
	}

	vary := rec.Header().Get("Vary")
	if !strings.Contains(vary, "Accept-Encoding") {
		t.Errorf("Vary = %q, want to contain Accept-Encoding", vary)
	}

	body := gzipDecompress(t, rec.Body)
	if body != `{"message":"hello world","count":42}` {
		t.Errorf("body = %q after decompression", body)
	}

	// Verify compression actually happened — compressed should be smaller for
	// non-trivial JSON.
	rawLen := len(`{"message":"hello world","count":42}`)
	if rec.Body.Len() >= rawLen {
		t.Errorf("compressed size %d >= raw size %d — compression ineffective", rec.Body.Len(), rawLen)
	}
}

func TestCompressionMiddleware_NoGzipHeader(t *testing.T) {
	handler := CompressionMiddleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))

	req := httptest.NewRequest("GET", "/api/test", nil)
	// No Accept-Encoding header — client doesn't support gzip.
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}

	ce := rec.Header().Get("Content-Encoding")
	if ce == "gzip" {
		t.Errorf("Content-Encoding = gzip but client didn't request it")
	}

	body := rec.Body.String()
	if body != `{"ok":true}` {
		t.Errorf("body = %q, want uncompressed JSON", body)
	}
}

func TestCompressionMiddleware_BinaryContentType(t *testing.T) {
	handler := CompressionMiddleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("fake-png-data-here"))
	}))

	req := httptest.NewRequest("GET", "/api/image", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}

	// Binary content types should NOT be compressed.
	ce := rec.Header().Get("Content-Encoding")
	if ce == "gzip" {
		t.Errorf("Content-Encoding = gzip but content is image/png (non-compressible)")
	}

	body := rec.Body.String()
	if body != "fake-png-data-here" {
		t.Errorf("body = %q, want uncompressed binary data", body)
	}
}

func TestCompressionMiddleware_HTMLContentType(t *testing.T) {
	handler := CompressionMiddleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("<html><body><h1>Dashboard</h1></body></html>"))
	}))

	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}

	ce := rec.Header().Get("Content-Encoding")
	if ce != "gzip" {
		t.Errorf("Content-Encoding = %q for HTML, want gzip", ce)
	}

	body := gzipDecompress(t, rec.Body)
	if !strings.Contains(body, "<html>") {
		t.Errorf("decompressed body missing HTML: %q", body)
	}
}

func TestCompressionMiddleware_EmptyBody(t *testing.T) {
	handler := CompressionMiddleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNoContent) // 204, no body
	}))

	req := httptest.NewRequest("DELETE", "/api/resource", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Errorf("status = %d, want 204", rec.Code)
	}

	// Content-Encoding should still be set (writeHeader path decided),
	// but empty body is fine.
}

func TestCompressionMiddleware_LargeJSONResponse(t *testing.T) {
	// Build a large JSON payload to verify compression ratio.
	largePayload := strings.Repeat(`{"key":"value","num":12345}`, 200) // ~6KB
	handler := CompressionMiddleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(largePayload))
	}))

	req := httptest.NewRequest("GET", "/api/big", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}

	body := gzipDecompress(t, rec.Body)
	if body != largePayload {
		t.Errorf("decompressed body mismatch (len=%d, want len=%d)", len(body), len(largePayload))
	}

	// Compression ratio should be significant for repetitive JSON.
	ratio := float64(rec.Body.Len()) / float64(len(largePayload))
	if ratio > 0.5 {
		t.Errorf("compression ratio %.2f > 0.5 — expected better compression for repetitive JSON", ratio)
	}
}

func TestCompressionMiddleware_StreamingResponse(t *testing.T) {
	// Multiple Write() calls should be compressed as a stream.
	handler := CompressionMiddleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`["item1",`))
		_, _ = w.Write([]byte(`"item2",`))
		_, _ = w.Write([]byte(`"item3"]`))
	}))

	req := httptest.NewRequest("GET", "/api/stream", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	body := gzipDecompress(t, rec.Body)
	if body != `["item1","item2","item3"]` {
		t.Errorf("body = %q after streaming decompression", body)
	}
}

func TestCompressionMiddleware_VaryHeader(t *testing.T) {
	handler := CompressionMiddleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))

	req := httptest.NewRequest("GET", "/api/test", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	vary := rec.Header().Get("Vary")
	if !strings.Contains(vary, "Accept-Encoding") {
		t.Errorf("Vary header = %q, should include Accept-Encoding for CDN/proxy correctness", vary)
	}
}

func TestCompressionMiddleware_StatusCodePreserved(t *testing.T) {
	codes := []int{200, 201, 400, 404, 500}
	for _, code := range codes {
		t.Run(http.StatusText(code), func(t *testing.T) {
			wantCode := code
			handler := CompressionMiddleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(wantCode)
				_, _ = w.Write([]byte(`{"error":"test"}`))
			}))

			req := httptest.NewRequest("GET", "/api/test", nil)
			req.Header.Set("Accept-Encoding", "gzip")
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if rec.Code != wantCode {
				t.Errorf("status = %d, want %d", rec.Code, wantCode)
			}
		})
	}
}

func TestIsCompressibleContentType(t *testing.T) {
	tests := []struct {
		contentType string
		want        bool
	}{
		{"application/json", true},
		{"application/json; charset=utf-8", true},
		{"text/html", true},
		{"text/html; charset=utf-8", true},
		{"text/plain", true},
		{"text/csv", true},
		{"text/yaml", true},
		{"application/yaml", true},
		{"application/x-yaml", true},
		{"application/xml", true},
		{"text/xml", true},
		{"application/javascript", true},
		{"text/javascript", true},
		{"text/css", true},
		{"", true}, // default — empty Content-Type is compressible
		{"image/png", false},
		{"image/jpeg", false},
		{"application/octet-stream", false},
		{"video/mp4", false},
		{"application/zip", false},
		{"application/pdf", false},
	}
	for _, tt := range tests {
		t.Run(tt.contentType, func(t *testing.T) {
			got := isCompressibleContentType(tt.contentType)
			if got != tt.want {
				t.Errorf("isCompressibleContentType(%q) = %v, want %v", tt.contentType, got, tt.want)
			}
		})
	}
}

// Test that gzip writer pool reuses writers (no panic, no leak).
func TestGzipWriterPool_Reuse(t *testing.T) {
	// Run many concurrent compression requests to exercise the pool.
	for i := 0; i < 100; i++ {
		handler := CompressionMiddleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"iter":` + strings.Repeat("x", 100) + `}`))
		}))
		req := httptest.NewRequest("GET", "/api/pool", nil)
		req.Header.Set("Accept-Encoding", "gzip")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("iter %d: status = %d", i, rec.Code)
		}
		// Verify decompression works.
		_ = gzipDecompress(t, rec.Body)
	}
}

// Test that the middleware doesn't double-compress and respects
// custom Content-Encoding set by upstream handlers.
func TestCompressionMiddleware_AlreadyCompressed(t *testing.T) {
	handler := CompressionMiddleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// Simulate an upstream handler that already set Content-Encoding.
		w.Header().Set("Content-Encoding", "identity")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"already":"done"}`))
	}))

	req := httptest.NewRequest("GET", "/api/test", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}

	// When upstream handler explicitly sets Content-Encoding (e.g., "identity"),
	// the middleware should respect it and NOT overwrite with gzip.
	ce := rec.Header().Get("Content-Encoding")
	if ce == "gzip" {
		t.Errorf("Content-Encoding = gzip but handler already set identity")
	}

	// Body should be uncompressed plain text.
	body := rec.Body.String()
	if body != `{"already":"done"}` {
		t.Errorf("body = %q, want uncompressed JSON", body)
	}
}
