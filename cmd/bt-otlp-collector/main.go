// bt-otlp-collector is a minimal OTLP/HTTP collector that accepts trace spans
// POSTed to /v1/traces and writes them to a structured log file.
//
// Usage:
//
//	bt-otlp-collector [--port 4318] [--logdir ~/.go-bt-evolve/logs/otlp]
//
// Set BT_OTLP_ENDPOINT=http://localhost:4318 in any BT binary's env to
// have traces forwarded to this collector. This provides the production
// validation artifact for the Observability maturity dimension.
//
// The collector logs each received span batch to a dated file:
//
//	otlp-traces-YYYY-MM-DD.log
//
// It also exposes /api/otlp-stats returning JSON stats about received
// traces for dashboard integration.
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"
)

var (
	receivedSpans   atomic.Uint64
	receivedBatches atomic.Uint64
	mu              sync.Mutex
	logFile         *os.File
	startTime       = time.Now()
)

func main() {
	port := "4318"
	if p := os.Getenv("BT_OTLP_COLLECTOR_PORT"); p != "" {
		port = p
	}
	logDir := filepath.Join(os.Getenv("HOME"), ".go-bt-evolve", "logs", "otlp")
	if d := os.Getenv("BT_OTLP_COLLECTOR_LOGDIR"); d != "" {
		logDir = d
	}

	if err := os.MkdirAll(logDir, 0755); err != nil {
		log.Fatalf("failed to create log dir %s: %v", logDir, err)
	}

	today := time.Now().Format("2006-01-02")
	logPath := filepath.Join(logDir, fmt.Sprintf("otlp-traces-%s.log", today))
	var err error
	logFile, err = os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		log.Fatalf("failed to open trace log: %v", err)
	}
	defer logFile.Close()

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/traces", handleTraces)
	mux.HandleFunc("/api/otlp-stats", handleStats)
	mux.HandleFunc("/api/health", handleHealth)

	addr := fmt.Sprintf(":%s", port)
	log.Printf("bt-otlp-collector starting on %s, logging to %s", addr, logPath)
	if err := http.ListenAndServe(addr, mux); err != nil {
		_ = logFile.Close()
		log.Fatalf("server error: %v", err)
	}
}

func handleTraces(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, fmt.Sprintf("read error: %v", err), http.StatusBadRequest)
		return
	}
	receivedBatches.Add(1)

	// Parse to count spans
	var payload map[string]any
	var spanCount int
	if err := json.Unmarshal(body, &payload); err == nil {
		if rss, ok := payload["resourceSpans"].([]any); ok {
			for _, rs := range rss {
				if rsMap, ok := rs.(map[string]any); ok {
					if sss, ok := rsMap["scopeSpans"].([]any); ok {
						for _, ss := range sss {
							if ssMap, ok := ss.(map[string]any); ok {
								if spans, ok := ssMap["spans"].([]any); ok {
									spanCount += len(spans)
									receivedSpans.Add(uint64(len(spans)))
								}
							}
						}
					}
				}
			}
		}
	}

	// Log the batch
	entry := map[string]any{
		"received_at": time.Now().UTC().Format(time.RFC3339Nano),
		"span_count":  spanCount,
		"payload":     json.RawMessage(body),
	}
	entryJSON, _ := json.Marshal(entry)

	mu.Lock()
	fmt.Fprintln(logFile, string(entryJSON))
	mu.Unlock()

	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func handleStats(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	uptime := time.Since(startTime).Truncate(time.Second).String()
	_ = json.NewEncoder(w).Encode(map[string]any{
		"batches_received": receivedBatches.Load(),
		"spans_received":   receivedSpans.Load(),
		"uptime":           uptime,
		"started_at":       startTime.UTC().Format(time.RFC3339),
		"status":           "running",
	})
}

func handleHealth(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "healthy"})
}
