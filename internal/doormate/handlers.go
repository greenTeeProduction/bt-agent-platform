package doormate

import (
	"encoding/json"
	"fmt"
	"net/http"
)

// Handler implements the REST API handlers for DoorMate endpoints.
type Handler struct {
	store *Store
	agent *PageAgent
}

// NewHandler instantiates a new Handler with the provided store and page agent.
func NewHandler(store *Store, agent *PageAgent) *Handler {
	return &Handler{store: store, agent: agent}
}

func (h *Handler) getProfile(r *http.Request) *UserProfile {
	profile, err := h.store.LoadProfile("default_user")
	if err != nil {
		return &UserProfile{ID: "default_user", PreferenceTags: []string{}, PreferredStyle: "visual"}
	}
	return profile
}

// HandleIntent parses the request body, processes the intent, saves the session/page,
// updates profile tags, and returns the correct response.
func (h *Handler) HandleIntent(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var body struct {
		Input string `json:"input"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	profile := h.getProfile(r)
	sess, page, err := h.agent.Process(body.Input, profile)
	if err != nil {
		http.Error(w, "generation failed", http.StatusInternalServerError)
		return
	}

	_ = h.store.SaveSession(sess)
	_ = h.store.SavePage(page)

	// Lightweight profile update based on intent
	hasTag := false
	for _, t := range profile.PreferenceTags {
		if t == sess.Intent {
			hasTag = true
			break
		}
	}
	if !hasTag && sess.Intent != "general" {
		profile.PreferenceTags = append(profile.PreferenceTags, sess.Intent)
		_ = h.store.SaveProfile(profile)
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"session_id": sess.ID,
		"intent":     sess.Intent,
		"bubbles":    sess.Bubbles,
		"page":       page,
		"profile":    profile,
	})
}

// HandleBookmark loads a page, toggles bookmark status, updates profile bookmarks,
// logs feedback, and returns success.
func (h *Handler) HandleBookmark(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var body struct {
		PageID string `json:"page_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	page, err := h.store.LoadPage(body.PageID)
	if err != nil {
		http.Error(w, "page not found", http.StatusNotFound)
		return
	}

	page.Bookmarked = !page.Bookmarked
	_ = h.store.SavePage(page)

	profile := h.getProfile(r)
	if page.Bookmarked {
		profile.BookmarkIDs = append(profile.BookmarkIDs, page.ID)
		_ = h.store.LogFeedback(&FeedbackEvent{Type: "bookmark", Value: page.ID, PageID: page.ID})
	} else {
		newList := []string{}
		for _, b := range profile.BookmarkIDs {
			if b != page.ID {
				newList = append(newList, b)
			}
		}
		profile.BookmarkIDs = newList
		_ = h.store.LogFeedback(&FeedbackEvent{Type: "unbookmark", Value: page.ID, PageID: page.ID})
	}
	_ = h.store.SaveProfile(profile)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"status": "success", "bookmarked": page.Bookmarked})
}

// HandleRate sets rating, saves page, logs feedback, and returns success.
func (h *Handler) HandleRate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var body struct {
		PageID string `json:"page_id"`
		Rating int    `json:"rating"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	page, err := h.store.LoadPage(body.PageID)
	if err != nil {
		http.Error(w, "page not found", http.StatusNotFound)
		return
	}

	page.Rating = body.Rating
	_ = h.store.SavePage(page)

	_ = h.store.LogFeedback(&FeedbackEvent{Type: "rate", Value: fmt.Sprintf("%d", body.Rating), PageID: page.ID})

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"status": "success", "rating": page.Rating})
}

// HandleProfile loads/saves user profile preferences.
func (h *Handler) HandleProfile(w http.ResponseWriter, r *http.Request) {
	profile := h.getProfile(r)

	if r.Method == http.MethodPost {
		var body struct {
			Tags  []string `json:"tags"`
			Style string   `json:"style"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err == nil {
			profile.PreferenceTags = body.Tags
			profile.PreferredStyle = body.Style
			_ = h.store.SaveProfile(profile)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(profile)
}
