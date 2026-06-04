package dashboard

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/nico/go-bt-evolve/internal/hitl"
)

// HandleHITLPending returns all pending HITL approval requests.
// GET /api/hitl/pending
func HandleHITLPending(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	store := hitl.DefaultStore
	if store == nil {
		json.NewEncoder(w).Encode([]any{})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(store.ListPending())
}

// HandleHITL routes HITL REST endpoints under /api/hitl/.
func HandleHITL(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/hitl")
	path = strings.Trim(path, "/")
	if path == "" || path == "pending" {
		if path == "pending" && r.Method == http.MethodGet {
			HandleHITLPending(w, r)
			return
		}
		http.NotFound(w, r)
		return
	}

	parts := strings.Split(path, "/")
	id := parts[0]
	if id == "" {
		http.NotFound(w, r)
		return
	}

	store := hitl.DefaultStore
	if store == nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(map[string]string{"error": "HITL store not initialized"})
		return
	}

	if len(parts) == 1 && r.Method == http.MethodGet {
		req, ok := store.Get(id)
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(req)
		return
	}

	if len(parts) == 2 {
		var body struct {
			Reviewer string `json:"reviewer"`
			Comment  string `json:"comment"`
			Reason   string `json:"reason"`
		}
		if r.Method == http.MethodPost {
			_ = json.NewDecoder(r.Body).Decode(&body)
		}
		if body.Reviewer == "" {
			body.Reviewer = "dashboard"
		}
		switch parts[1] {
		case "approve":
			if r.Method != http.MethodPost {
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
				return
			}
			req, err := store.Approve(id, body.Reviewer, body.Comment)
			writeHITLResult(w, req, err)
			return
		case "reject":
			if r.Method != http.MethodPost {
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
				return
			}
			reason := body.Reason
			if reason == "" {
				reason = body.Comment
			}
			req, err := store.Reject(id, body.Reviewer, reason)
			writeHITLResult(w, req, err)
			return
		case "escalate":
			if r.Method != http.MethodPost {
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
				return
			}
			reason := body.Reason
			if reason == "" {
				reason = body.Comment
			}
			req, err := store.Escalate(id, body.Reviewer, reason)
			writeHITLResult(w, req, err)
			return
		}
	}

	http.NotFound(w, r)
}

func writeHITLResult(w http.ResponseWriter, req *hitl.Request, err error) {
	w.Header().Set("Content-Type", "application/json")
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	json.NewEncoder(w).Encode(req)
}
