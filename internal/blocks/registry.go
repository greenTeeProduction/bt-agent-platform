package blocks

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/nico/go-bt-evolve/internal/evolution"
)

// Registry stores reusable building blocks (built-in + persisted custom).
type Registry struct {
	mu     sync.RWMutex
	dir    string
	blocks map[string]*Block
}

// DefaultRegistry is the process-wide block registry with built-ins loaded.
var DefaultRegistry = NewRegistry("")

// NewRegistry creates a registry. If dir is non-empty, custom blocks load from dir/blocks/.
func NewRegistry(dir string) *Registry {
	r := &Registry{
		dir:    dir,
		blocks: make(map[string]*Block),
	}
	for _, b := range builtinBlocks() {
		r.blocks[b.ID] = &b
	}
	if dir != "" {
		_ = r.loadFromDisk()
	}
	return r
}

// Register adds or replaces a block. Persists when the registry has a directory.
func (r *Registry) Register(b Block) error {
	if b.ID == "" {
		return fmt.Errorf("block id required")
	}
	if b.Tree == nil {
		return fmt.Errorf("block %q: tree required", b.ID)
	}
	if errs := b.Tree.Validate(); len(errs) > 0 {
		return fmt.Errorf("block %q invalid: %v", b.ID, errs)
	}
	r.mu.Lock()
	cp := b
	if cp.Category == "" {
		cp.Category = CategoryCustom
	}
	r.blocks[cp.ID] = &cp
	r.mu.Unlock()
	if r.dir != "" {
		return r.saveBlock(&cp)
	}
	return nil
}

// Get returns a deep copy of the block tree, or nil.
func (r *Registry) Get(id string) *Block {
	r.mu.RLock()
	b, ok := r.blocks[id]
	r.mu.RUnlock()
	if !ok || b == nil {
		return nil
	}
	return &Block{
		ID:          b.ID,
		Name:        b.Name,
		Description: b.Description,
		Category:    b.Category,
		Tree:        cloneTree(b.Tree),
		Mutable:     b.Mutable,
		Version:     b.Version,
	}
}

// List returns metadata for all blocks sorted by id.
func (r *Registry) List() []Block {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Block, 0, len(r.blocks))
	for _, b := range r.blocks {
		out = append(out, Block{
			ID:          b.ID,
			Name:        b.Name,
			Description: b.Description,
			Category:    b.Category,
			Mutable:     b.Mutable,
			Version:     b.Version,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// IDs returns all registered block ids.
func (r *Registry) IDs() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	ids := make([]string, 0, len(r.blocks))
	for id := range r.blocks {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

func (r *Registry) blocksDir() string {
	return filepath.Join(r.dir, "blocks")
}

func (r *Registry) loadFromDisk() error {
	dir := r.blocksDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		name := filepath.Base(e.Name())
		if name != e.Name() {
			continue
		}
		root, err := os.OpenRoot(dir)
		if err != nil {
			continue
		}
		data, err := root.ReadFile(name)
		root.Close()
		if err != nil {
			continue
		}
		var b Block
		if err := json.Unmarshal(data, &b); err != nil || b.ID == "" || b.Tree == nil {
			continue
		}
		r.mu.Lock()
		r.blocks[b.ID] = &b
		r.mu.Unlock()
	}
	return nil
}

func (r *Registry) saveBlock(b *Block) error {
	dir := r.blocksDir()
	if err := os.MkdirAll(dir, 0750); err != nil {
		return err
	}
	safe := filepath.Base(b.ID)
	if safe == "." || safe == ".." {
		safe = "custom_block"
	}
	path := filepath.Join(dir, safe+".json")
	data, err := json.MarshalIndent(b, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func cloneTree(t *evolution.SerializableNode) *evolution.SerializableNode {
	if t == nil {
		return nil
	}
	c := &evolution.SerializableNode{
		Type:        t.Type,
		Name:        t.Name,
		Description: t.Description,
		MaxRetries:  t.MaxRetries,
		TimeoutMs:   t.TimeoutMs,
	}
	if t.Metadata != nil {
		c.Metadata = make(map[string]any)
		for k, v := range t.Metadata {
			c.Metadata[k] = v
		}
	}
	if t.Edges != nil {
		c.Edges = make([]evolution.TypedEdge, len(t.Edges))
		copy(c.Edges, t.Edges)
	}
	for _, ch := range t.Children {
		c.Children = append(c.Children, *cloneTree(&ch))
	}
	return c
}

// IsEvolutionMutable reports whether block id may be mutated by the evolver.
func (r *Registry) IsEvolutionMutable(id string) bool {
	b := r.Get(id)
	if b == nil {
		return false
	}
	return b.Mutable && !strings.HasPrefix(b.ID, "core:")
}

// Freeze sets Mutable=false for a block (builtin or custom).
func (r *Registry) Freeze(id string) error {
	r.mu.Lock()
	b, ok := r.blocks[id]
	if !ok || b == nil {
		r.mu.Unlock()
		return fmt.Errorf("block %q not found", id)
	}
	b.Mutable = false
	cp := *b
	r.mu.Unlock()
	return r.Register(cp)
}

// PromoteVersion copies a block to custom:<suffix>_vN with incremented version.
func (r *Registry) PromoteVersion(srcID, destID string) (*Block, error) {
	src := r.Get(srcID)
	if src == nil {
		return nil, fmt.Errorf("source block %q not found", srcID)
	}
	if destID == "" {
		destID = "custom:" + strings.TrimPrefix(srcID, "core:") + "_v" + fmt.Sprint(src.Version+1)
	}
	promoted := Block{
		ID:              destID,
		Name:            src.Name + " (promoted)",
		Description:     "Promoted from " + srcID,
		Category:        CategoryCustom,
		Tree:            cloneTree(src.Tree),
		Mutable:         true,
		Version:         src.Version + 1,
		PromotedVersion: src.Version,
	}
	if err := r.Register(promoted); err != nil {
		return nil, err
	}
	return &promoted, nil
}

// FilterEvolutionMutations drops ops that target frozen or core builtin blocks.
func (r *Registry) FilterEvolutionMutations(ops []evolution.MutationOp) []evolution.MutationOp {
	if len(ops) == 0 {
		return ops
	}
	out := make([]evolution.MutationOp, 0, len(ops))
	for _, op := range ops {
		bid := blockIDFromOp(op)
		if bid != "" && !r.IsEvolutionMutable(bid) {
			continue
		}
		if strings.HasPrefix(op.Target, "core:") {
			continue
		}
		out = append(out, op)
	}
	return out
}
