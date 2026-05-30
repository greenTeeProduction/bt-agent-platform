package tracing

import (
	"net/http"
	"time"
)

// ─── HTTP Tracing Middleware ─────────────────────────────────────────────────

// responseWriter wraps http.ResponseWriter to capture the status code.
type responseWriter struct {
	http.ResponseWriter
	statusCode int
	wroteHeader bool
}

func (rw *responseWriter) WriteHeader(code int) {
	if !rw.wroteHeader {
		rw.statusCode = code
		rw.wroteHeader = true
	}
	rw.ResponseWriter.WriteHeader(code)
}

func (rw *responseWriter) Write(b []byte) (int, error) {
	if !rw.wroteHeader {
		rw.statusCode = http.StatusOK
		rw.wroteHeader = true
	}
	return rw.ResponseWriter.Write(b)
}

// TracingMiddleware wraps an http.Handler and creates a span for every request.
// Uses the global tracer — a noop by default if SetGlobalTracer was never called.
// Span attributes recorded: http.method, http.url, http.status_code, http.duration_ms.
// For requests taking >5 seconds, a "slow_request" event is added.
func TracingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		spanName := "http:" + r.Method + " " + r.URL.Path
		ctx, span := StartSpan(r.Context(), spanName)
		defer span.End()

		span.SetAttribute("http.method", r.Method)
		span.SetAttribute("http.url", r.URL.String())

		rw := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}
		next.ServeHTTP(rw, r.WithContext(ctx))

		duration := time.Since(start)
		span.SetAttribute("http.status_code", IntAttr("http.status_code", rw.statusCode).Value)
		span.SetAttribute("http.duration_ms", DurationAttr("http.duration_ms", duration).Value)

		if duration > 5*time.Second {
			span.AddEvent("slow_request",
				StringAttr("threshold", "5s"),
				DurationAttr("actual", duration),
			)
		}
	})
}

// FlushWriter optionally supports http.Flusher.
var _ http.Flusher = (*responseWriter)(nil)

func (rw *responseWriter) Flush() {
	if f, ok := rw.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}
