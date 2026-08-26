package main

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/nico/go-bt-evolve/internal/blackboard"
)

// GET /api/blackboard?scope=session|agent|run&scope_id=...&prefix=...&limit=50
func handleBlackboard(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}
	if dashAgentRunner == nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "agent runner not configured"})
		return
	}

	scopeKind := r.URL.Query().Get("scope")
	scopeID := r.URL.Query().Get("scope_id")
	prefix := r.URL.Query().Get("prefix")
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 {
		limit = 50
	}

	scope, err := parseBlackboardScope(scopeKind, scopeID)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	mgr := dashAgentRunner.BoardManager()
	entries, err := mgr.List(scope, prefix, limit)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"scope":    scopeKind,
		"scope_id": scopeID,
		"prefix":   prefix,
		"count":    len(entries),
		"entries":  entries,
	})
}

// GET /api/blackboard/scopes?scope=session|agent
func handleBlackboardScopes(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}
	if dashAgentRunner == nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "agent runner not configured"})
		return
	}

	scopeKind := r.URL.Query().Get("scope")
	var kind blackboard.ScopeKind
	switch scopeKind {
	case string(blackboard.ScopeSession):
		kind = blackboard.ScopeSession
	case string(blackboard.ScopeAgent):
		kind = blackboard.ScopeAgent
	default:
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "scope must be session or agent"})
		return
	}

	ids, err := dashAgentRunner.BoardManager().ListPersistedScopeIDs(kind)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"scope": scopeKind,
		"count": len(ids),
		"ids":   ids,
	})
}

func parseBlackboardScope(kind, id string) (blackboard.Scope, error) {
	if id == "" {
		return blackboard.Scope{}, errScopeIDRequired
	}
	switch kind {
	case string(blackboard.ScopeRun):
		return blackboard.Scope{Kind: blackboard.ScopeRun, ID: id}, nil
	case string(blackboard.ScopeSession):
		return blackboard.Scope{Kind: blackboard.ScopeSession, ID: id}, nil
	case string(blackboard.ScopeAgent):
		return blackboard.Scope{Kind: blackboard.ScopeAgent, ID: id}, nil
	default:
		return blackboard.Scope{}, errUnknownScope
	}
}

type scopeError string

func (e scopeError) Error() string { return string(e) }

const (
	errScopeIDRequired = scopeError("scope_id required")
	errUnknownScope    = scopeError("unknown scope (use run, session, or agent)")
)
