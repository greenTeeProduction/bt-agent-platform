// Package persona implements the per-user personalization layer (ADR-010
// Phase 1): user profiles, per-user workspaces, interaction logs, and habit
// mining. It generalizes the DoorMate UserProfile into a platform-wide
// concept so agents can adapt to the specific human they work with.
//
// The package is dependency-free within the platform (standard library only):
// callers pass the users root directory (agent.UsersDir()) and, optionally,
// an embedding function for habit mining, keeping persona importable from
// every layer without cycles.
package persona

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Profile captures what the platform has learned about one user.
type Profile struct {
	ID             string   `json:"id"`
	PreferenceTags []string `json:"preference_tags,omitempty"`
	PreferredStyle string   `json:"preferred_style,omitempty"` // "visual", "minimal", "detailed"
	// PromptHints are style instructions injected into generated ChainAction
	// prompts ("answer in German", "prefer tables over prose", ...).
	PromptHints []string `json:"prompt_hints,omitempty"`
	// ToolHabits counts observed tool usage so tree generation can prefer
	// the tools this user actually works with.
	ToolHabits map[string]int `json:"tool_habits,omitempty"`
	Approval   ApprovalPolicy `json:"approval"`
	CreatedAt  int64          `json:"created_at"`
	UpdatedAt  int64          `json:"updated_at"`
}

// ApprovalPolicy bounds how autonomously the platform may act for this user
// (ADR-010: HITL default-on, per-user cap on auto-created automations).
type ApprovalPolicy struct {
	// AutoApproveAutomations skips the HITL gate for auto-compiled
	// automations. Default false: every proposal needs explicit approval.
	AutoApproveAutomations bool `json:"auto_approve_automations"`
	// MaxAutoCreatedAgents caps how many auto-created scheduled agents may
	// be active at once for this user.
	MaxAutoCreatedAgents int `json:"max_auto_created_agents"`
}

// defaultMaxAutoCreatedAgents is the automation-spam guard default.
const defaultMaxAutoCreatedAgents = 3

// ContextBlock renders the profile as a compact prompt-injectable block, in
// the same spirit as agent.MemoryStore's ContextBlock. Empty profile → "".
func (p *Profile) ContextBlock() string {
	if p == nil {
		return ""
	}
	var b strings.Builder
	if p.PreferredStyle != "" {
		fmt.Fprintf(&b, "Preferred output style: %s\n", p.PreferredStyle)
	}
	if len(p.PreferenceTags) > 0 {
		fmt.Fprintf(&b, "User preferences: %s\n", strings.Join(p.PreferenceTags, ", "))
	}
	for _, hint := range p.PromptHints {
		fmt.Fprintf(&b, "Style hint: %s\n", hint)
	}
	if b.Len() == 0 {
		return ""
	}
	return "## User Profile (" + p.ID + ")\n" + b.String()
}

// Store persists profiles under <usersRoot>/<user>/profile.json with ADR-003
// atomic writes. It is safe for concurrent use.
type Store struct {
	root string
	mu   sync.Mutex
}

// NewStore creates a profile store rooted at the users directory
// (typically agent.UsersDir() → ~/.go-bt-evolve/users).
func NewStore(usersRoot string) (*Store, error) {
	if strings.TrimSpace(usersRoot) == "" {
		return nil, fmt.Errorf("persona store: empty users root")
	}
	if err := os.MkdirAll(usersRoot, 0755); err != nil {
		return nil, fmt.Errorf("persona store: %w", err)
	}
	return &Store{root: usersRoot}, nil
}

// Root returns the users root directory.
func (s *Store) Root() string { return s.root }

// Workspace returns the per-user workspace for a user ID, creating nothing.
func (s *Store) Workspace(user string) Workspace {
	return Workspace{Root: filepath.Join(s.root, SanitizeUserID(user)), User: user}
}

// Load reads a user's profile, returning a fresh default profile when none
// has been persisted yet (a new user is not an error).
func (s *Store) Load(user string) (*Profile, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.loadLocked(user)
}

func (s *Store) loadLocked(user string) (*Profile, error) {
	if strings.TrimSpace(user) == "" {
		return nil, fmt.Errorf("persona: empty user id")
	}
	path := s.Workspace(user).ProfilePath()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			now := time.Now().Unix()
			return &Profile{
				ID:        user,
				Approval:  ApprovalPolicy{MaxAutoCreatedAgents: defaultMaxAutoCreatedAgents},
				CreatedAt: now,
				UpdatedAt: now,
			}, nil
		}
		return nil, fmt.Errorf("read profile %q: %w", user, err)
	}
	var p Profile
	if err := json.Unmarshal(data, &p); err != nil {
		return nil, fmt.Errorf("unmarshal profile %q: %w", user, err)
	}
	if p.ID == "" {
		p.ID = user
	}
	return &p, nil
}

// Save persists a profile atomically (write .tmp → rename).
func (s *Store) Save(p *Profile) error {
	if p == nil || strings.TrimSpace(p.ID) == "" {
		return fmt.Errorf("persona: profile requires a user id")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.saveLocked(p)
}

func (s *Store) saveLocked(p *Profile) error {
	p.UpdatedAt = time.Now().Unix()
	ws := s.Workspace(p.ID)
	if err := os.MkdirAll(ws.Root, 0755); err != nil {
		return fmt.Errorf("create user workspace: %w", err)
	}
	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal profile: %w", err)
	}
	path := ws.ProfilePath()
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return fmt.Errorf("write tmp profile: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("rename profile: %w", err)
	}
	return nil
}

// Update loads a profile, applies fn, and saves the result under one lock so
// concurrent read-modify-write cycles cannot lose updates.
func (s *Store) Update(user string, fn func(*Profile)) (*Profile, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, err := s.loadLocked(user)
	if err != nil {
		return nil, err
	}
	fn(p)
	if err := s.saveLocked(p); err != nil {
		return nil, err
	}
	return p, nil
}

// AddPreferenceTag appends a preference tag if not already present.
func (s *Store) AddPreferenceTag(user, tag string) (*Profile, error) {
	tag = strings.TrimSpace(tag)
	if tag == "" {
		return nil, fmt.Errorf("persona: empty preference tag")
	}
	return s.Update(user, func(p *Profile) {
		for _, existing := range p.PreferenceTags {
			if strings.EqualFold(existing, tag) {
				return
			}
		}
		p.PreferenceTags = append(p.PreferenceTags, tag)
	})
}

// Workspace is the per-user persistence layout (ADR-010):
//
//	users/<user>/
//	├── profile.json        — Profile
//	├── interactions.jsonl  — interaction log (Log)
//	├── trees/              — generated/evolved personal trees
//	├── goals/              — persistent goal queue (Phase 2)
//	├── memory/             — user-scoped MemoryStore
//	├── reflections/        — user-scoped run reflections
//	└── experience/         — per-user ExperienceBank
type Workspace struct {
	Root string
	User string
}

func (w Workspace) ProfilePath() string      { return filepath.Join(w.Root, "profile.json") }
func (w Workspace) InteractionsPath() string { return filepath.Join(w.Root, "interactions.jsonl") }
func (w Workspace) TreesDir() string         { return filepath.Join(w.Root, "trees") }
func (w Workspace) GoalsDir() string         { return filepath.Join(w.Root, "goals") }
func (w Workspace) MemoryDir() string        { return filepath.Join(w.Root, "memory") }
func (w Workspace) ReflectionsDir() string   { return filepath.Join(w.Root, "reflections") }
func (w Workspace) ExperienceDir() string    { return filepath.Join(w.Root, "experience") }

// SanitizeUserID maps a user identifier to a cross-platform-safe directory
// name fragment (same policy as evolution.TreeFileName's ID sanitization).
func SanitizeUserID(user string) string {
	var b strings.Builder
	for _, r := range strings.TrimSpace(user) {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '.':
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	if b.Len() == 0 {
		return "_anonymous"
	}
	id := b.String()
	if strings.Trim(id, ".") == "" {
		// "." resolves to the store root and ".." to its parent when joined
		// as a workspace path segment; prefix all-dot ids to keep them inert.
		return "_" + id
	}
	return id
}
