package blackboard

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

type scopeFile struct {
	Entries map[string]Entry `json:"entries"`
}

func (m *Manager) EnablePersistence(baseDir string) error {
	if m == nil {
		return fmt.Errorf("blackboard manager is nil")
	}
	baseDir = strings.TrimSpace(baseDir)
	if baseDir == "" {
		return fmt.Errorf("blackboard persist dir required")
	}
	m.operations.Lock()
	defer m.operations.Unlock()
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := os.MkdirAll(filepath.Join(baseDir, "session"), 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(baseDir, "agent"), 0o755); err != nil {
		return err
	}
	m.persistDir = baseDir
	return nil
}

func isPersistentScope(kind ScopeKind) bool {
	return kind == ScopeSession || kind == ScopeAgent
}

func (m *Manager) persistFile(scope Scope) string {
	if m == nil || m.persistDir == "" || !isPersistentScope(scope.Kind) {
		return ""
	}
	sub := string(scope.Kind)
	return filepath.Join(m.persistDir, sub, "v2."+base64.RawURLEncoding.EncodeToString([]byte(scope.ID))+".json")
}

func loadScopeFile(path string, s *scopedStore) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var file scopeFile
	if err := json.Unmarshal(data, &file); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries = make(map[string]Entry, len(file.Entries))
	s.totalBytes = 0
	for k, e := range file.Entries {
		k = normalizeKey(k)
		if k == "" {
			continue
		}
		e.Key = k
		if e.ContentType == "" {
			e.ContentType = "text"
		}
		if e.Summary == "" {
			e.Summary = truncateSummary(e.Value, 500)
		}
		e.SizeBytes = len(e.Value)
		s.entries[k] = e
		s.totalBytes += int64(len(e.Value))
	}
	return nil
}

func (m *Manager) persistScope(scope Scope, s *scopedStore) error {
	path := m.persistFile(scope)
	if path == "" || s == nil {
		return nil
	}
	snap := s.snapshot()
	file := scopeFile{Entries: snap}
	data, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".scope-*.tmp")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if _, err = f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func safeFilename(id string) string {
	var b strings.Builder
	for _, r := range id {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	out := b.String()
	if out == "" {
		return "unknown"
	}
	return out
}

func (s *scopedStore) snapshot() map[string]Entry {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make(map[string]Entry, len(s.entries))
	maps.Copy(out, s.entries)
	return out
}

// ListPersistedScopeIDs returns scope IDs that have on-disk storage (session/agent only).
func (m *Manager) ListPersistedScopeIDs(kind ScopeKind) ([]string, error) {
	if m == nil || m.persistDir == "" {
		return nil, fmt.Errorf("blackboard persistence not enabled")
	}
	if !isPersistentScope(kind) {
		return nil, fmt.Errorf("scope kind %q is not persisted", kind)
	}
	dir := filepath.Join(m.persistDir, string(kind))
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		id := strings.TrimSuffix(e.Name(), ".json")
		if encoded, ok := strings.CutPrefix(id, "v2."); ok {
			decoded, err := base64.RawURLEncoding.DecodeString(encoded)
			if err != nil {
				return nil, err
			}
			id = string(decoded)
		}
		out = append(out, id)
	}
	slices.Sort(out)
	return slices.Compact(out), nil
}

// Legacy filenames can be attributed safely only to IDs the old encoder left
// unchanged. Ambiguous lossy IDs must never silently inherit another scope.
func (m *Manager) loadScope(scope Scope, s *scopedStore) error {
	path := m.persistFile(scope)
	if _, err := os.Stat(path); os.IsNotExist(err) {
		legacy := filepath.Join(m.persistDir, string(scope.Kind), safeFilename(scope.ID)+".json")
		if _, legacyErr := os.Stat(legacy); legacyErr == nil {
			if safeFilename(scope.ID) != scope.ID || strings.Contains(scope.ID, "_") {
				return fmt.Errorf("legacy scope %q is ambiguous; explicit migration required", scope.ID)
			}
			return loadScopeFile(legacy, s)
		}
	}
	return loadScopeFile(path, s)
}
