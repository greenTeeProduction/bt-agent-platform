// Package agent provides the agent definition, registry, and lifecycle management
// for the Go BT framework. Agents are defined in YAML and managed through a registry
// that supports create, list, run, test, schedule, and versioning.
package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

// Definition is a YAML-based agent definition.
// Agents are behavior trees with metadata, input/output contracts, and quality gates.
type Definition struct {
	Name        string            `yaml:"name" json:"name"`
	Description string            `yaml:"description" json:"description"`
	Version     string            `yaml:"version" json:"version"`
	Tree        string            `yaml:"tree" json:"tree"`               // tree ID: "domain:code_review", "finance:pitch_agent", etc.
	Schedule    string            `yaml:"schedule,omitempty" json:"schedule,omitempty"` // cron expression or "on_demand"
	Inputs      []InputSpec       `yaml:"inputs,omitempty" json:"inputs,omitempty"`
	Outputs     []OutputSpec      `yaml:"outputs,omitempty" json:"outputs,omitempty"`
	Quality     *QualitySpec      `yaml:"quality,omitempty" json:"quality,omitempty"`
	Metadata    map[string]string `yaml:"metadata,omitempty" json:"metadata,omitempty"`
	CreatedAt   time.Time         `yaml:"created_at" json:"created_at"`
	UpdatedAt   time.Time         `yaml:"updated_at" json:"updated_at"`
}

// InputSpec defines an input parameter for the agent.
type InputSpec struct {
	Name        string `yaml:"name" json:"name"`
	Type        string `yaml:"type" json:"type"` // text, file, json
	Required    bool   `yaml:"required" json:"required"`
	Default     string `yaml:"default,omitempty" json:"default,omitempty"`
	Description string `yaml:"description,omitempty" json:"description,omitempty"`
}

// OutputSpec defines an expected output format.
type OutputSpec struct {
	Name        string `yaml:"name" json:"name"`
	Type        string `yaml:"type" json:"type"` // markdown, json, text
	Description string `yaml:"description,omitempty" json:"description,omitempty"`
}

// QualitySpec defines quality gate requirements for agent output.
type QualitySpec struct {
	MinLength         int      `yaml:"min_length" json:"min_length"`
	RequiredSections  []string `yaml:"required_sections,omitempty" json:"required_sections,omitempty"`
	RequiredKeywords  []string `yaml:"required_keywords,omitempty" json:"required_keywords,omitempty"`
	BlockedPatterns   []string `yaml:"blocked_patterns,omitempty" json:"blocked_patterns,omitempty"`
}

// State represents the current state of a deployed agent.
type State string

const (
	StateCreated   State = "created"
	StateRunning   State = "running"
	StatePaused    State = "paused"
	StateError     State = "error"
	StateCompleted State = "completed"
)

// Instance is a running instance of an agent definition.
type Instance struct {
	ID          string    `json:"id"`
	Definition  Definition `json:"definition"`
	State       State     `json:"state"`
	RunCount    int       `json:"run_count"`
	SuccessRate float64   `json:"success_rate"`
	LastRun     time.Time `json:"last_run"`
	LastError   string    `json:"last_error,omitempty"`
}

// Registry manages agent definitions and instances.
type Registry struct {
	mu        sync.RWMutex
	dir       string
	instances map[string]*Instance
}

// NewRegistry creates a new agent registry.
func NewRegistry(dir string) (*Registry, error) {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("create registry dir: %w", err)
	}
	r := &Registry{
		dir:       dir,
		instances: make(map[string]*Instance),
	}
	return r, r.loadAll()
}

// Create creates a new agent from a definition and adds it to the registry.
func (r *Registry) Create(def Definition) (*Instance, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.instances[def.Name]; exists {
		return nil, fmt.Errorf("agent %q already exists", def.Name)
	}

	now := time.Now()
	def.CreatedAt = now
	def.UpdatedAt = now
	if def.Version == "" {
		def.Version = "1.0.0"
	}

	inst := &Instance{
		ID:         fmt.Sprintf("agent_%d", now.UnixMilli()),
		Definition: def,
		State:      StateCreated,
	}

	// Persist to disk
	if err := r.saveDef(def); err != nil {
		return nil, fmt.Errorf("save definition: %w", err)
	}

	r.instances[def.Name] = inst
	return inst, nil
}

// Get returns an agent instance by name.
func (r *Registry) Get(name string) (*Instance, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	inst, ok := r.instances[name]
	if !ok {
		return nil, fmt.Errorf("agent %q not found", name)
	}
	return inst, nil
}

// List returns all agent instances.
func (r *Registry) List() []*Instance {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make([]*Instance, 0, len(r.instances))
	for _, inst := range r.instances {
		result = append(result, inst)
	}
	return result
}

// UpdateState updates the state of a running agent.
func (r *Registry) UpdateState(name string, state State, lastError string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	inst, ok := r.instances[name]
	if !ok {
		return fmt.Errorf("agent %q not found", name)
	}
	inst.State = state
	inst.LastError = lastError
	inst.LastRun = time.Now()
	inst.RunCount++

	def := inst.Definition
	def.UpdatedAt = time.Now()
	inst.Definition = def
	return r.saveDef(def)
}

// Delete removes an agent from the registry.
func (r *Registry) Delete(name string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, ok := r.instances[name]; !ok {
		return fmt.Errorf("agent %q not found", name)
	}

	defPath := filepath.Join(r.dir, name+".yaml")
	if err := os.Remove(defPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove definition file: %w", err)
	}

	delete(r.instances, name)
	return nil
}

// saveDef persists an agent definition to disk as YAML.
func (r *Registry) saveDef(def Definition) error {
	path := filepath.Join(r.dir, def.Name+".yaml")
	data, err := yaml.Marshal(def)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	return os.WriteFile(path, data, 0644)
}

// loadAll loads all agent definitions from disk.
func (r *Registry) loadAll() error {
	entries, err := os.ReadDir(r.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".yaml" {
			continue
		}
		path := filepath.Join(r.dir, entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var def Definition
		if err := yaml.Unmarshal(data, &def); err != nil {
			continue
		}
		r.instances[def.Name] = &Instance{
			ID:         fmt.Sprintf("agent_%d", def.CreatedAt.UnixMilli()),
			Definition: def,
			State:      StateCreated,
		}
	}
	return nil
}
