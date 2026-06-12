package doormate

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestHandleIntent(t *testing.T) {
	storeDir := t.TempDir()
	store, err := NewStore(storeDir)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	agent := NewPageAgent(nil)
	handler := NewHandler(store, agent)

	reqBody, _ := json.Marshal(map[string]string{"input": "lock security"})
	req := httptest.NewRequest("POST", "/api/doormate/intent", bytes.NewReader(reqBody))
	rr := httptest.NewRecorder()

	handler.HandleIntent(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rr.Code)
	}

	var res map[string]interface{}
	if err := json.Unmarshal(rr.Body.Bytes(), &res); err != nil {
		t.Fatalf("invalid json response: %v", err)
	}

	sessionID, ok := res["session_id"].(string)
	if !ok || sessionID == "" {
		t.Errorf("expected session_id in response")
	}

	intent, ok := res["intent"].(string)
	if !ok || intent != "security" {
		t.Errorf("expected security intent, got %v", res["intent"])
	}

	// Verify session and page are saved
	sess, err := store.LoadSession(sessionID)
	if err != nil {
		t.Errorf("failed to load saved session: %v", err)
	}
	if sess.Intent != "security" {
		t.Errorf("expected saved session intent to be security, got %s", sess.Intent)
	}

	pageData, ok := res["page"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected page in response")
	}
	pageID, ok := pageData["id"].(string)
	if !ok || pageID == "" {
		t.Fatalf("expected page id in response")
	}

	page, err := store.LoadPage(pageID)
	if err != nil {
		t.Errorf("failed to load saved page: %v", err)
	}
	if page.SessionID != sessionID {
		t.Errorf("expected page session ID %s, got %s", sessionID, page.SessionID)
	}

	// Verify profile tags are updated
	profile, err := store.LoadProfile("default_user")
	if err != nil {
		t.Errorf("failed to load profile: %v", err)
	}
	hasTag := false
	for _, tag := range profile.PreferenceTags {
		if tag == "security" {
			hasTag = true
			break
		}
	}
	if !hasTag {
		t.Errorf("expected profile to be updated with security tag")
	}
}

func TestHandleBookmark(t *testing.T) {
	storeDir := t.TempDir()
	store, err := NewStore(storeDir)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	agent := NewPageAgent(nil)
	handler := NewHandler(store, agent)

	// First, generate a page to bookmark
	profile := &UserProfile{ID: "default_user"}
	sess, page, err := agent.Process("lock security", profile)
	if err != nil {
		t.Fatalf("failed to process: %v", err)
	}
	if err := store.SaveSession(sess); err != nil {
		t.Fatalf("failed to save session: %v", err)
	}
	if err := store.SavePage(page); err != nil {
		t.Fatalf("failed to save page: %v", err)
	}

	// Test bookmarking (toggling to true)
	reqBody, _ := json.Marshal(map[string]string{"page_id": page.ID})
	req := httptest.NewRequest("POST", "/api/doormate/bookmark", bytes.NewReader(reqBody))
	rr := httptest.NewRecorder()

	handler.HandleBookmark(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rr.Code)
	}

	var res map[string]interface{}
	if err := json.Unmarshal(rr.Body.Bytes(), &res); err != nil {
		t.Fatalf("invalid json response: %v", err)
	}
	if res["status"] != "success" {
		t.Errorf("expected status success, got %v", res["status"])
	}
	if res["bookmarked"] != true {
		t.Errorf("expected bookmarked to be true")
	}

	// Verify page bookmark status is saved
	savedPage, err := store.LoadPage(page.ID)
	if err != nil {
		t.Fatalf("failed to load page: %v", err)
	}
	if !savedPage.Bookmarked {
		t.Errorf("expected page to be bookmarked")
	}

	// Verify profile bookmarks are updated
	savedProfile, err := store.LoadProfile("default_user")
	if err != nil {
		t.Fatalf("failed to load profile: %v", err)
	}
	hasBookmark := false
	for _, bID := range savedProfile.BookmarkIDs {
		if bID == page.ID {
			hasBookmark = true
			break
		}
	}
	if !hasBookmark {
		t.Errorf("expected profile to have bookmark ID %s", page.ID)
	}

	// Verify feedback is logged
	files, err := os.ReadDir(filepath.Join(storeDir, "feedback"))
	if err != nil {
		t.Fatalf("failed to read feedback dir: %v", err)
	}
	if len(files) == 0 {
		t.Errorf("expected feedback to be logged")
	}

	// Test unbookmarking (toggling to false)
	reqBody2, _ := json.Marshal(map[string]string{"page_id": page.ID})
	req2 := httptest.NewRequest("POST", "/api/doormate/bookmark", bytes.NewReader(reqBody2))
	rr2 := httptest.NewRecorder()

	handler.HandleBookmark(rr2, req2)

	if rr2.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rr2.Code)
	}

	var res2 map[string]interface{}
	if err := json.Unmarshal(rr2.Body.Bytes(), &res2); err != nil {
		t.Fatalf("invalid json response: %v", err)
	}
	if res2["bookmarked"] != false {
		t.Errorf("expected bookmarked to be false after toggle")
	}

	// Verify page bookmark status is saved as false
	savedPage2, _ := store.LoadPage(page.ID)
	if savedPage2.Bookmarked {
		t.Errorf("expected page to not be bookmarked")
	}

	// Verify profile bookmarks are updated (removed)
	savedProfile2, _ := store.LoadProfile("default_user")
	for _, bID := range savedProfile2.BookmarkIDs {
		if bID == page.ID {
			t.Errorf("expected profile to not have bookmark ID %s", page.ID)
		}
	}
}

func TestHandleRate(t *testing.T) {
	storeDir := t.TempDir()
	store, err := NewStore(storeDir)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	agent := NewPageAgent(nil)
	handler := NewHandler(store, agent)

	// First, generate a page to rate
	profile := &UserProfile{ID: "default_user"}
	sess, page, err := agent.Process("lock security", profile)
	if err != nil {
		t.Fatalf("failed to process: %v", err)
	}
	if err := store.SaveSession(sess); err != nil {
		t.Fatalf("failed to save session: %v", err)
	}
	if err := store.SavePage(page); err != nil {
		t.Fatalf("failed to save page: %v", err)
	}

	// Test rating
	reqBody, _ := json.Marshal(map[string]interface{}{"page_id": page.ID, "rating": 5})
	req := httptest.NewRequest("POST", "/api/doormate/rate", bytes.NewReader(reqBody))
	rr := httptest.NewRecorder()

	handler.HandleRate(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rr.Code)
	}

	var res map[string]interface{}
	if err := json.Unmarshal(rr.Body.Bytes(), &res); err != nil {
		t.Fatalf("invalid json response: %v", err)
	}
	if res["status"] != "success" {
		t.Errorf("expected status success, got %v", res["status"])
	}
	if int(res["rating"].(float64)) != 5 {
		t.Errorf("expected rating to be 5, got %v", res["rating"])
	}

	// Verify page rating status is saved
	savedPage, err := store.LoadPage(page.ID)
	if err != nil {
		t.Fatalf("failed to load page: %v", err)
	}
	if savedPage.Rating != 5 {
		t.Errorf("expected page rating to be 5, got %d", savedPage.Rating)
	}

	// Verify feedback is logged
	files, err := os.ReadDir(filepath.Join(storeDir, "feedback"))
	if err != nil {
		t.Fatalf("failed to read feedback dir: %v", err)
	}
	if len(files) == 0 {
		t.Errorf("expected feedback to be logged")
	}
}

func TestHandleProfile(t *testing.T) {
	storeDir := t.TempDir()
	store, err := NewStore(storeDir)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	agent := NewPageAgent(nil)
	handler := NewHandler(store, agent)

	// Test GET profile (loads user profile preferences)
	req := httptest.NewRequest("GET", "/api/doormate/profile", nil)
	rr := httptest.NewRecorder()

	handler.HandleProfile(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rr.Code)
	}

	var loadedProfile UserProfile
	if err := json.Unmarshal(rr.Body.Bytes(), &loadedProfile); err != nil {
		t.Fatalf("invalid json response: %v", err)
	}
	if loadedProfile.ID != "default_user" {
		t.Errorf("expected profile ID default_user, got %s", loadedProfile.ID)
	}

	// Test POST profile (saves user profile preferences)
	prefTags := []string{"security", "automation"}
	reqBody, _ := json.Marshal(map[string]interface{}{
		"tags":  prefTags,
		"style": "minimal",
	})
	req2 := httptest.NewRequest("POST", "/api/doormate/profile", bytes.NewReader(reqBody))
	rr2 := httptest.NewRecorder()

	handler.HandleProfile(rr2, req2)

	if rr2.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rr2.Code)
	}

	var savedProfile UserProfile
	if err := json.Unmarshal(rr2.Body.Bytes(), &savedProfile); err != nil {
		t.Fatalf("invalid json response: %v", err)
	}
	if savedProfile.PreferredStyle != "minimal" {
		t.Errorf("expected preferred style minimal, got %s", savedProfile.PreferredStyle)
	}
	if len(savedProfile.PreferenceTags) != 2 || savedProfile.PreferenceTags[0] != "security" {
		t.Errorf("expected preference tags [security, automation], got %v", savedProfile.PreferenceTags)
	}

	// Verify it was persisted
	persistedProfile, err := store.LoadProfile("default_user")
	if err != nil {
		t.Fatalf("failed to load profile: %v", err)
	}
	if persistedProfile.PreferredStyle != "minimal" {
		t.Errorf("expected persisted preferred style minimal, got %s", persistedProfile.PreferredStyle)
	}
}
