// Package agent provides the agent definition, registry, and lifecycle management
// for the Go BT framework. Agents are defined in YAML and managed through a registry
// that supports create, list, run, test, schedule, and versioning.
package agent

import (
	"context"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"sync"
	"time"

	"github.com/nico/go-bt-evolve/internal/reliability"
	"github.com/nico/go-bt-evolve/internal/util"
	"gopkg.in/yaml.v3"
)

// Definition is a YAML-based agent definition.
// Agents are behavior trees with metadata, input/output contracts, and quality gates.
type Definition struct {
	Name        string            `yaml:"name" json:"name"`
	Description string            `yaml:"description" json:"description"`
	Version     string            `yaml:"version" json:"version"`
	Tree        string            `yaml:"tree" json:"tree"`                             // tree ID: "domain:code_review", "finance:pitch_agent", etc.
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
	MinLength        int      `yaml:"min_length" json:"min_length"`
	RequiredSections []string `yaml:"required_sections,omitempty" json:"required_sections,omitempty"`
	RequiredKeywords []string `yaml:"required_keywords,omitempty" json:"required_keywords,omitempty"`
	BlockedPatterns  []string `yaml:"blocked_patterns,omitempty" json:"blocked_patterns,omitempty"`
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
	ID          string     `json:"id"`
	Definition  Definition `json:"definition"`
	State       State      `json:"state"`
	RunCount    int        `json:"run_count"`
	SuccessRate float64    `json:"success_rate"`
	LastRun     time.Time  `json:"last_run"`
	LastError   string     `json:"last_error,omitempty"`
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
	return r.create(def, false)
}

// EnsureDefinition creates a definition or accepts an exact existing one.
// Runtime timestamps and a default version are normalized; owner, task, tree,
// schedule, inputs and every other configuration field must match. Creation
// checks disk under a bounded sidecar lock, including with a stale registry.
func (r *Registry) EnsureDefinition(def Definition) (*Instance, error) {
	return r.create(def, true)
}

func (r *Registry) create(def Definition, acceptExact bool) (*Instance, error) {
	if err := ValidateName(def.Name); err != nil {
		return nil, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	path := filepath.Join(r.dir, def.Name+".yaml")
	release, err := reliability.AcquireFileLockWithContext(ctx, path)
	if err != nil {
		return nil, err
	}
	defer release()
	data, err := util.ReadPersistenceFile(path)
	if err == nil {
		var existing Definition
		if err := yaml.Unmarshal(data, &existing); err != nil {
			return nil, fmt.Errorf("existing agent definition: %w", err)
		}
		if !acceptExact {
			return nil, fmt.Errorf("agent %q already exists", def.Name)
		}
		if !sameDefinition(existing, def) {
			return nil, fmt.Errorf("agent %q already exists with different configuration", def.Name)
		}
		inst := &Instance{ID: fmt.Sprintf("agent_%d", existing.CreatedAt.UnixMilli()), Definition: existing, State: StateCreated}
		if previous := r.instances[def.Name]; previous != nil {
			inst = cloneInstance(previous)
			inst.Definition = existing
		}
		r.instances[def.Name] = inst
		return cloneInstance(inst), nil
	}
	if !os.IsNotExist(err) {
		return nil, fmt.Errorf("read agent definition: %w", err)
	}
	if _, exists := r.instances[def.Name]; exists {
		return nil, fmt.Errorf("agent %q already exists in registry but is missing on disk", def.Name)
	}

	now := time.Now()
	def = cloneDefinition(def)
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
	return cloneInstance(inst), nil
}

func sameDefinition(a, b Definition) bool {
	a.CreatedAt, b.CreatedAt = time.Time{}, time.Time{}
	a.UpdatedAt, b.UpdatedAt = time.Time{}, time.Time{}
	if a.Version == "" {
		a.Version = "1.0.0"
	}
	if b.Version == "" {
		b.Version = "1.0.0"
	}
	return reflect.DeepEqual(a, b)
}

// Get returns an agent instance by name.
func (r *Registry) Get(name string) (*Instance, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	inst, ok := r.instances[name]
	if !ok {
		return nil, fmt.Errorf("agent %q not found", name)
	}
	return cloneInstance(inst), nil
}

// List returns all agent instances.
func (r *Registry) List() []*Instance {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make([]*Instance, 0, len(r.instances))
	for _, inst := range r.instances {
		result = append(result, cloneInstance(inst))
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

// UpdateSchedule updates and persists an agent schedule.
func (r *Registry) UpdateSchedule(name, schedule string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	inst, ok := r.instances[name]
	if !ok {
		return fmt.Errorf("agent %q not found", name)
	}
	inst.Definition.Schedule = schedule
	inst.Definition.UpdatedAt = time.Now()
	return r.saveDef(inst.Definition)
}

// Delete removes an agent from the registry.
func (r *Registry) Delete(name string) error {
	if err := ValidateName(name); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, ok := r.instances[name]; !ok {
		return fmt.Errorf("agent %q not found", name)
	}

	if err := RemoveDefinitionFile(r.dir, name); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove definition file: %w", err)
	}

	delete(r.instances, name)
	return nil
}

// ReloadFromDisk refreshes agent definitions from YAML files on disk.
// Runtime stats (run count, success rate, last run) are preserved for agents that still exist.
func (r *Registry) ReloadFromDisk() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	prev := r.instances
	r.instances = make(map[string]*Instance)
	if err := r.loadAll(); err != nil {
		r.instances = prev
		return err
	}
	for name, inst := range r.instances {
		if old, ok := prev[name]; ok {
			inst.ID = old.ID
			inst.State = old.State
			inst.RunCount = old.RunCount
			inst.SuccessRate = old.SuccessRate
			inst.LastRun = old.LastRun
			inst.LastError = old.LastError
		}
	}
	return nil
}

// saveDef persists an agent definition to disk as YAML.
func (r *Registry) saveDef(def Definition) error {
	if err := ValidateName(def.Name); err != nil {
		return err
	}
	data, err := yaml.Marshal(def)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	return WriteDefinitionFile(r.dir, def.Name, data)
}

// loadAll loads all agent definitions from disk.
func (r *Registry) loadAll() error {
	root, err := os.OpenRoot(r.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	defer root.Close()
	dir, err := root.Open(".")
	if err != nil {
		return err
	}
	defer dir.Close()
	entries, err := dir.ReadDir(-1)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	for _, entry := range entries {
		if !entry.Type().IsRegular() || filepath.Ext(entry.Name()) != ".yaml" {
			continue
		}
		data, err := root.ReadFile(entry.Name())
		if err != nil {
			continue
		}
		var def Definition
		if err := yaml.Unmarshal(data, &def); err != nil {
			continue
		}
		if err := ValidateName(def.Name); err != nil {
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

func cloneDefinition(d Definition) Definition {
	d.Inputs = slices.Clone(d.Inputs)
	d.Outputs = slices.Clone(d.Outputs)
	d.Metadata = maps.Clone(d.Metadata)
	if d.Quality != nil {
		q := *d.Quality
		q.RequiredSections = slices.Clone(q.RequiredSections)
		q.RequiredKeywords = slices.Clone(q.RequiredKeywords)
		q.BlockedPatterns = slices.Clone(q.BlockedPatterns)
		d.Quality = &q
	}
	return d
}
func cloneInstance(inst *Instance) *Instance {
	cp := *inst
	cp.Definition = cloneDefinition(inst.Definition)
	return &cp
}
