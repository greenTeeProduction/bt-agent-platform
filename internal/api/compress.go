// Package api provides API design primitives for the BT platform:
// content types, OpenAPI 3.0 spec generation, response validation,
// deprecation headers, and response compression.
package api

import (
	"compress/gzip"
	"io"
	"net/http"
	"strings"
	"sync"
)

// CompressibleTypes lists MIME types that benefit from gzip compression.
// Text-based formats (JSON, YAML, HTML, plain text, CSV, XML) and JavaScript.
var CompressibleTypes = []string{
	"application/json",
	"application/yaml",
	"application/x-yaml",
	"text/plain",
	"text/html",
	"text/csv",
	"text/yaml",
	"application/xml",
	"text/xml",
	"application/javascript",
	"text/javascript",
	"text/css",
}

// gzipWriterPool reuses gzip.Writer instances to reduce allocation pressure
// under concurrent load.
var gzipWriterPool = sync.Pool{
	New: func() any {
		w, _ := gzip.NewWriterLevel(io.Discard, gzip.DefaultCompression)
		return w
	},
}

// gzipResponseWriter wraps http.ResponseWriter to transparently compress
// responses when the client supports gzip and the Content-Type is compressible.
// If the Content-Type is not compressible, the gzip writer is bypassed and
// raw bytes flow to the underlying ResponseWriter.
type gzipResponseWriter struct {
	http.ResponseWriter
	Writer       io.Writer
	compressible bool // set after WriteHeader determines Content-Type
	started      bool
}

// WriteHeader captures the status code and detects whether the Content-Type
// is compressible. If not compressible, it removes the Content-Encoding header
// and falls back to the underlying ResponseWriter for all subsequent writes.
func (w *gzipResponseWriter) WriteHeader(code int) {
	if w.started {
		return
	}
	w.started = true

	// Detect Content-Type and decide whether to compress.
	ct := w.ResponseWriter.Header().Get("Content-Encoding")
	hasCustomEncoding := ct != "" && ct != "gzip"

	contentType := w.ResponseWriter.Header().Get("Content-Type")
	w.compressible = !hasCustomEncoding && isCompressibleContentType(contentType)

	if !w.compressible {
		// Content-Type not compressible or custom Content-Encoding already set —
		// remove Content-Encoding (if we set it), write uncompressed.
		if ct == "gzip" || ct == "" {
			w.ResponseWriter.Header().Del("Content-Encoding")
		}
		w.ResponseWriter.WriteHeader(code)
		return
	}
	w.ResponseWriter.WriteHeader(code)
}

// Write compresses data through the gzip writer, or passes through raw
// bytes when the Content-Type is not compressible.
func (w *gzipResponseWriter) Write(b []byte) (int, error) {
	// If Write is called before WriteHeader (common for small responses),
	// check compressibility first.
	if !w.started {
		ct := w.ResponseWriter.Header().Get("Content-Encoding")
		hasCustomEncoding := ct != "" && ct != "gzip"

		contentType := w.ResponseWriter.Header().Get("Content-Type")
		w.compressible = !hasCustomEncoding && isCompressibleContentType(contentType)
		w.started = true

		if !w.compressible {
			if ct == "gzip" || ct == "" {
				w.ResponseWriter.Header().Del("Content-Encoding")
			}
		}
	}

	if !w.compressible {
		return w.ResponseWriter.Write(b)
	}
	return w.Writer.Write(b)
}

// compressionResponseWriter wraps the response to add Vary: Accept-Encoding
// and detect client gzip support.
type compressionResponseWriter struct {
	http.ResponseWriter
}

func (w *compressionResponseWriter) WriteHeader(code int) {
	w.ResponseWriter.Header().Add("Vary", "Accept-Encoding")
	w.ResponseWriter.WriteHeader(code)
}

// CompressionMiddleware transparently compresses HTTP responses with gzip
// when the client sends Accept-Encoding: gzip and the response Content-Type
// is a compressible text format (JSON, YAML, HTML, plain text, etc.).
//
// Benefits:
//   - Reduces response body size by 70-90% for JSON API responses
//   - Transparent to clients — Content-Encoding: gzip is set automatically
//   - Conservative: only compresses text-based MIME types; binary types pass through
//   - Uses sync.Pool for gzip.Writer reuse under concurrent load
//   - Adds Vary: Accept-Encoding for correct CDN/proxy caching
//
// Stack position: outermost layer (after MetricsMiddleware). Compression should
// be the last middleware before the network — it wraps the entire response.
func CompressionMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Check if client accepts gzip encoding.
		if !strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
			next.ServeHTTP(w, r)
			return
		}

		// Set up the compression pipeline.
		// 1. Add Vary: Accept-Encoding for correct caching behavior.
		crw := &compressionResponseWriter{ResponseWriter: w}

		// 2. Set Content-Encoding header (may be removed if Content-Type is not compressible).
		crw.Header().Set("Content-Encoding", "gzip")

		// 3. Acquire a gzip.Writer from the pool.
		gz := gzipWriterPool.Get().(*gzip.Writer)
		gz.Reset(crw)

		// 4. Wrap in our response writer that decides whether to compress.
		grw := &gzipResponseWriter{ResponseWriter: crw, Writer: gz}
		defer func() {
			if grw.compressible {
				gz.Close()
			} else {
				// No data was written to gz — discard the empty gzip stream
				// to avoid appending gzip headers to the raw response.
				gz.Reset(io.Discard)
			}
			gzipWriterPool.Put(gz)
		}()

		next.ServeHTTP(grw, r)
	})
}

// isCompressibleContentType checks whether the given Content-Type (with optional
// charset or boundary parameters) matches one of the CompressibleTypes.
// Returns true if the type is compressible, false otherwise.
// Empty Content-Type defaults to compressible (text/plain semantics).
func isCompressibleContentType(contentType string) bool {
	if contentType == "" {
		return true // default to compressible
	}
	// Strip parameters: "application/json; charset=utf-8" → "application/json"
	mediaType := strings.ToLower(strings.TrimSpace(contentType))
	if idx := strings.IndexByte(mediaType, ';'); idx != -1 {
		mediaType = strings.TrimSpace(mediaType[:idx])
	}
	for _, ct := range CompressibleTypes {
		if mediaType == ct {
			return true
		}
	}
	return false
}
