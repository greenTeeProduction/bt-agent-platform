package doormate

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Store implements a thread-safe JSON file-based storage persistence layer.
type Store struct {
	mu  sync.RWMutex
	dir string
}

// NewStore creates a new Store instance and ensures that all required subdirectories exist.
func NewStore(dir string) (*Store, error) {
	subdirs := []string{"sessions", "pages", "profiles", "feedback"}
	for _, sub := range subdirs {
		subPath := filepath.Join(dir, sub)
		if err := os.MkdirAll(subPath, 0750); err != nil {
			return nil, fmt.Errorf("failed to create subdirectory %s: %w", sub, err)
		}
	}
	return &Store{dir: dir}, nil
}

// atomicWrite writes data to path using the atomic-rename pattern to prevent data corruption.
// It assumes the caller holds the appropriate write lock.
func (s *Store) atomicWrite(path string, data interface{}) error {
	bytes, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal JSON: %w", err)
	}

	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, bytes, 0640); err != nil {
		return fmt.Errorf("failed to write temporary file: %w", err)
	}

	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("failed to rename temporary file to destination: %w", err)
	}

	return nil
}

// SaveSession saves an active user intent session.
func (s *Store) SaveSession(sess *IntentSession) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	sess.UpdatedAt = time.Now().Unix()
	if sess.CreatedAt == 0 {
		sess.CreatedAt = sess.UpdatedAt
	}

	path := filepath.Join(s.dir, "sessions", sess.ID+".json")
	return s.atomicWrite(path, sess)
}

// LoadSession loads an active user intent session by ID.
func (s *Store) LoadSession(id string) (*IntentSession, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	path := filepath.Join(s.dir, "sessions", id+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read session file: %w", err)
	}

	var sess IntentSession
	if err := json.Unmarshal(data, &sess); err != nil {
		return nil, fmt.Errorf("failed to unmarshal session JSON: %w", err)
	}

	return &sess, nil
}

// SavePage saves a generated page.
func (s *Store) SavePage(page *GeneratedPage) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if page.CreatedAt == 0 {
		page.CreatedAt = time.Now().Unix()
	}

	path := filepath.Join(s.dir, "pages", page.ID+".json")
	return s.atomicWrite(path, page)
}

// LoadPage loads a generated page by ID.
func (s *Store) LoadPage(id string) (*GeneratedPage, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	path := filepath.Join(s.dir, "pages", id+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read page file: %w", err)
	}

	var page GeneratedPage
	if err := json.Unmarshal(data, &page); err != nil {
		return nil, fmt.Errorf("failed to unmarshal page JSON: %w", err)
	}

	return &page, nil
}

// SaveProfile saves a user profile.
func (s *Store) SaveProfile(prof *UserProfile) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	prof.UpdatedAt = time.Now().Unix()

	path := filepath.Join(s.dir, "profiles", prof.ID+".json")
	return s.atomicWrite(path, prof)
}

// LoadProfile loads a user profile by ID. Returns a clean default profile if it doesn't exist yet.
func (s *Store) LoadProfile(id string) (*UserProfile, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	path := filepath.Join(s.dir, "profiles", id+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &UserProfile{
				ID:             id,
				PreferenceTags: []string{},
				BookmarkIDs:    []string{},
				PreferredStyle: "visual",
				UpdatedAt:      time.Now().Unix(),
			}, nil
		}
		return nil, fmt.Errorf("failed to read profile file: %w", err)
	}

	var prof UserProfile
	if err := json.Unmarshal(data, &prof); err != nil {
		return nil, fmt.Errorf("failed to unmarshal profile JSON: %w", err)
	}

	return &prof, nil
}

// LogFeedback logs a feedback event.
func (s *Store) LogFeedback(evt *FeedbackEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if evt.Timestamp == 0 {
		evt.Timestamp = time.Now().Unix()
	}
	if evt.ID == "" {
		evt.ID = fmt.Sprintf("feed-%d", time.Now().UnixNano())
	}

	path := filepath.Join(s.dir, "feedback", evt.ID+".json")
	return s.atomicWrite(path, evt)
}
