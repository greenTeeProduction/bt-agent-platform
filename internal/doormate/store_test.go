package doormate

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNewStoreCreatesSubdirectories(t *testing.T) {
	tempDir := t.TempDir()

	_, err := NewStore(tempDir)
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}

	requiredSubdirs := []string{"sessions", "pages", "profiles", "feedback"}
	for _, sub := range requiredSubdirs {
		path := filepath.Join(tempDir, sub)
		info, err := os.Stat(path)
		if err != nil {
			t.Errorf("expected subdirectory %s to exist, but got error: %v", sub, err)
			continue
		}
		if !info.IsDir() {
			t.Errorf("expected %s to be a directory, but it is not", sub)
		}
	}
}

func TestStoreSaveAndLoadSession(t *testing.T) {
	tempDir := t.TempDir()
	store, err := NewStore(tempDir)
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}

	session := &IntentSession{
		ID:              "sess-test-123",
		RawInput:        "enable lock security",
		Intent:          "security",
		SelectedBubbles: []string{"Biometric Lock"},
		Bubbles:         []string{"Biometric Lock", "Smart Deadbolt"},
		PageIDs:         []string{"page-test-123"},
	}

	// Save session
	if err := store.SaveSession(session); err != nil {
		t.Fatalf("SaveSession failed: %v", err)
	}

	// Verify CreatedAt and UpdatedAt are set
	if session.CreatedAt == 0 || session.UpdatedAt == 0 {
		t.Errorf("expected CreatedAt and UpdatedAt to be set, got CreatedAt=%d, UpdatedAt=%d", session.CreatedAt, session.UpdatedAt)
	}

	// Load session
	loaded, err := store.LoadSession(session.ID)
	if err != nil {
		t.Fatalf("LoadSession failed: %v", err)
	}

	// Verify fields
	if loaded.ID != session.ID {
		t.Errorf("expected ID %q, got %q", session.ID, loaded.ID)
	}
	if loaded.RawInput != session.RawInput {
		t.Errorf("expected RawInput %q, got %q", session.RawInput, loaded.RawInput)
	}
	if loaded.Intent != session.Intent {
		t.Errorf("expected Intent %q, got %q", session.Intent, loaded.Intent)
	}
	if len(loaded.SelectedBubbles) != 1 || loaded.SelectedBubbles[0] != "Biometric Lock" {
		t.Errorf("expected SelectedBubbles [Biometric Lock], got %v", loaded.SelectedBubbles)
	}
	if len(loaded.Bubbles) != 2 || loaded.Bubbles[1] != "Smart Deadbolt" {
		t.Errorf("expected Bubbles [Biometric Lock, Smart Deadbolt], got %v", loaded.Bubbles)
	}
	if len(loaded.PageIDs) != 1 || loaded.PageIDs[0] != "page-test-123" {
		t.Errorf("expected PageIDs [page-test-123], got %v", loaded.PageIDs)
	}
	if loaded.CreatedAt != session.CreatedAt || loaded.UpdatedAt != session.UpdatedAt {
		t.Errorf("timestamps mismatch: expected CreatedAt=%d, UpdatedAt=%d; got CreatedAt=%d, UpdatedAt=%d", session.CreatedAt, session.UpdatedAt, loaded.CreatedAt, loaded.UpdatedAt)
	}

	// Test loading non-existent session
	_, err = store.LoadSession("non-existent")
	if err == nil {
		t.Error("expected error loading non-existent session, got nil")
	}
}

func TestStoreSaveAndLoadPage(t *testing.T) {
	tempDir := t.TempDir()
	store, err := NewStore(tempDir)
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}

	page := &GeneratedPage{
		ID:        "page-test-456",
		SessionID: "sess-test-123",
		Schema: PageSchema{
			Title:      "Smart Lock & Door Security Blueprint",
			Summary:    "Detailed assessment of secure entry methods.",
			TemplateID: "recommendation",
			Blocks: []Block{
				{
					Type:    "overview",
					Title:   "Overview Block",
					Content: "This is some content.",
				},
			},
			FollowUps: []string{"Follow up 1"},
		},
		Bookmarked: true,
		Rating:     4,
	}

	// Save page
	if err := store.SavePage(page); err != nil {
		t.Fatalf("SavePage failed: %v", err)
	}

	// Verify CreatedAt is set
	if page.CreatedAt == 0 {
		t.Error("expected CreatedAt to be set, got 0")
	}

	// Load page
	loaded, err := store.LoadPage(page.ID)
	if err != nil {
		t.Fatalf("LoadPage failed: %v", err)
	}

	// Verify fields
	if loaded.ID != page.ID {
		t.Errorf("expected ID %q, got %q", page.ID, loaded.ID)
	}
	if loaded.SessionID != page.SessionID {
		t.Errorf("expected SessionID %q, got %q", page.SessionID, loaded.SessionID)
	}
	if loaded.Schema.Title != page.Schema.Title {
		t.Errorf("expected Schema.Title %q, got %q", page.Schema.Title, loaded.Schema.Title)
	}
	if len(loaded.Schema.Blocks) != 1 || loaded.Schema.Blocks[0].Type != "overview" {
		t.Errorf("expected 1 block of type 'overview', got %d blocks", len(loaded.Schema.Blocks))
	}
	if !loaded.Bookmarked {
		t.Error("expected Bookmarked to be true, got false")
	}
	if loaded.Rating != 4 {
		t.Errorf("expected Rating 4, got %d", loaded.Rating)
	}
	if loaded.CreatedAt != page.CreatedAt {
		t.Errorf("expected CreatedAt %d, got %d", page.CreatedAt, loaded.CreatedAt)
	}

	// Test loading non-existent page
	_, err = store.LoadPage("non-existent")
	if err == nil {
		t.Error("expected error loading non-existent page, got nil")
	}
}

func TestStoreSaveAndLoadProfile(t *testing.T) {
	tempDir := t.TempDir()
	store, err := NewStore(tempDir)
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}

	profileID := "user-test-789"

	// Load non-existent profile (should return a clean default profile)
	loadedDefault, err := store.LoadProfile(profileID)
	if err != nil {
		t.Fatalf("LoadProfile failed for non-existent profile: %v", err)
	}
	if loadedDefault.ID != profileID {
		t.Errorf("expected default profile ID %q, got %q", profileID, loadedDefault.ID)
	}
	if len(loadedDefault.PreferenceTags) != 0 {
		t.Errorf("expected empty PreferenceTags, got %v", loadedDefault.PreferenceTags)
	}
	if len(loadedDefault.BookmarkIDs) != 0 {
		t.Errorf("expected empty BookmarkIDs, got %v", loadedDefault.BookmarkIDs)
	}
	if loadedDefault.PreferredStyle != "visual" {
		t.Errorf("expected default PreferredStyle 'visual', got %q", loadedDefault.PreferredStyle)
	}
	if loadedDefault.UpdatedAt == 0 {
		t.Error("expected UpdatedAt to be set on default profile, got 0")
	}

	// Save profile
	profile := &UserProfile{
		ID:             profileID,
		PreferenceTags: []string{"security", "modern"},
		BookmarkIDs:    []string{"page-test-456"},
		PreferredStyle: "minimal",
	}

	if err := store.SaveProfile(profile); err != nil {
		t.Fatalf("SaveProfile failed: %v", err)
	}

	// Verify UpdatedAt is set
	if profile.UpdatedAt == 0 {
		t.Error("expected UpdatedAt to be set, got 0")
	}

	// Load profile
	loaded, err := store.LoadProfile(profileID)
	if err != nil {
		t.Fatalf("LoadProfile failed: %v", err)
	}

	// Verify fields
	if loaded.ID != profile.ID {
		t.Errorf("expected ID %q, got %q", profile.ID, loaded.ID)
	}
	if len(loaded.PreferenceTags) != 2 || loaded.PreferenceTags[0] != "security" {
		t.Errorf("expected PreferenceTags [security, modern], got %v", loaded.PreferenceTags)
	}
	if len(loaded.BookmarkIDs) != 1 || loaded.BookmarkIDs[0] != "page-test-456" {
		t.Errorf("expected BookmarkIDs [page-test-456], got %v", loaded.BookmarkIDs)
	}
	if loaded.PreferredStyle != "minimal" {
		t.Errorf("expected PreferredStyle 'minimal', got %q", loaded.PreferredStyle)
	}
	if loaded.UpdatedAt != profile.UpdatedAt {
		t.Errorf("expected UpdatedAt %d, got %d", profile.UpdatedAt, loaded.UpdatedAt)
	}
}

func TestStoreLogFeedback(t *testing.T) {
	tempDir := t.TempDir()
	store, err := NewStore(tempDir)
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}

	evt := &FeedbackEvent{
		SessionID: "sess-test-123",
		PageID:    "page-test-456",
		Type:      "bookmark",
		Value:     "page-test-456",
	}

	// Log feedback
	if err := store.LogFeedback(evt); err != nil {
		t.Fatalf("LogFeedback failed: %v", err)
	}

	// Verify ID and Timestamp are generated/set
	if evt.ID == "" {
		t.Error("expected ID to be generated, got empty string")
	}
	if evt.Timestamp == 0 {
		t.Error("expected Timestamp to be set, got 0")
	}

	// Verify file exists in feedback directory
	path := filepath.Join(tempDir, "feedback", evt.ID+".json")
	if _, err := os.Stat(path); err != nil {
		t.Errorf("expected feedback file to exist at %s, got error: %v", path, err)
	}

	// Log feedback with explicit ID and Timestamp
	evt2 := &FeedbackEvent{
		ID:        "explicit-id",
		SessionID: "sess-test-123",
		PageID:    "page-test-456",
		Type:      "rate",
		Value:     "5",
		Timestamp: 123456789,
	}

	if err := store.LogFeedback(evt2); err != nil {
		t.Fatalf("LogFeedback failed: %v", err)
	}

	if evt2.ID != "explicit-id" {
		t.Errorf("expected ID 'explicit-id', got %q", evt2.ID)
	}
	if evt2.Timestamp != 123456789 {
		t.Errorf("expected Timestamp 123456789, got %d", evt2.Timestamp)
	}

	path2 := filepath.Join(tempDir, "feedback", "explicit-id.json")
	if _, err := os.Stat(path2); err != nil {
		t.Errorf("expected feedback file to exist at %s, got error: %v", path2, err)
	}
}

func TestStoreConcurrency(t *testing.T) {
	tempDir := t.TempDir()
	store, err := NewStore(tempDir)
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}

	// Run concurrent saves and loads to verify thread safety (no race conditions)
	const numGoroutines = 20
	done := make(chan bool, numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			profile := &UserProfile{
				ID:             "user-concurrent",
				PreferenceTags: []string{"tag"},
				PreferredStyle: "visual",
			}
			_ = store.SaveProfile(profile)
			_, _ = store.LoadProfile("user-concurrent")
			done <- true
		}(i)
	}

	for i := 0; i < numGoroutines; i++ {
		<-done
	}
}
