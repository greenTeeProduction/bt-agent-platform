package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// CatalogEntry is a lightweight listing for the agent marketplace.
type CatalogEntry struct {
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Version     string    `json:"version"`
	Tree        string    `json:"tree"`
	Category    string    `json:"category"`
	Tags        []string  `json:"tags"`
	Installed   bool      `json:"installed"`
	InstalledAt time.Time `json:"installed_at,omitempty"`
	Score       float64   `json:"score,omitempty"`
}

// Catalog manages the agent marketplace — browsing, searching, installing, sharing.
type Catalog struct {
	reg     *Registry
	tmplDir string
}

// NewCatalog creates a catalog backed by the registry and template directory.
func NewCatalog(reg *Registry, tmplDir string) *Catalog {
	return &Catalog{reg: reg, tmplDir: tmplDir}
}

// ListInstalled returns all installed agents with their current state.
func (c *Catalog) ListInstalled() []CatalogEntry {
	instances := c.reg.List()
	entries := make([]CatalogEntry, 0, len(instances))
	for _, inst := range instances {
		def := inst.Definition
		entry := CatalogEntry{
			Name:        def.Name,
			Description: def.Description,
			Version:     def.Version,
			Tree:        def.Tree,
			Category:    def.Metadata["category"],
			Tags:        splitTags(def.Metadata["tags"]),
			Installed:   true,
			InstalledAt: def.CreatedAt,
		}
		entries = append(entries, entry)
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name < entries[j].Name
	})
	return entries
}

// ListTemplates returns available agent templates.
func (c *Catalog) ListTemplates() ([]CatalogEntry, error) {
	entries, err := os.ReadDir(c.tmplDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	result := make([]CatalogEntry, 0, 32)
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".yaml" {
			continue
		}
		name := e.Name()[:len(e.Name())-5]
		data, err := os.ReadFile(filepath.Join(c.tmplDir, e.Name()))
		if err != nil {
			continue
		}

		// Extract basic info from YAML
		desc := extractYAMLField(string(data), "description")
		tree := extractYAMLField(string(data), "tree")
		category := extractYAMLField(string(data), "category")
		tags := extractYAMLField(string(data), "tags")

		result = append(result, CatalogEntry{
			Name:        name,
			Description: desc,
			Tree:        tree,
			Category:    category,
			Tags:        splitTags(tags),
			Installed:   c.isInstalled(name),
		})
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Name < result[j].Name
	})
	return result, nil
}

// Search finds agents matching a query (name, description, tags, category).
func (c *Catalog) Search(query string) []CatalogEntry {
	query = strings.ToLower(query)
	var results []CatalogEntry

	// Search installed agents
	for _, entry := range c.ListInstalled() {
		if c.matches(entry, query) {
			results = append(results, entry)
		}
	}

	// Search templates
	templates, _ := c.ListTemplates()
	for _, tmpl := range templates {
		if !tmpl.Installed && c.matches(tmpl, query) {
			results = append(results, tmpl)
		}
	}

	return results
}

// InstallFromTemplate installs an agent from a template.
func (c *Catalog) InstallFromTemplate(name string) (*Instance, error) {
	tmplPath := filepath.Join(c.tmplDir, name+".yaml")
	data, err := os.ReadFile(tmplPath)
	if err != nil {
		return nil, fmt.Errorf("template %q not found: %w", name, err)
	}

	// Parse the YAML template into a Definition
	def := Definition{
		Name:        name,
		Description: extractYAMLField(string(data), "description"),
		Version:     extractYAMLField(string(data), "version"),
		Tree:        extractYAMLField(string(data), "tree"),
		Schedule:    extractYAMLField(string(data), "schedule"),
		Metadata: map[string]string{
			"category": extractYAMLField(string(data), "category"),
			"tags":     extractYAMLField(string(data), "tags"),
		},
	}

	return c.reg.Create(def)
}

// Export exports an agent definition to a portable YAML file.
func (c *Catalog) Export(name, outputPath string) error {
	inst, err := c.reg.Get(name)
	if err != nil {
		return err
	}

	data := fmt.Sprintf(`# BT Agent: %s
# Exported: %s
name: %s
description: %s
version: %s
tree: %s
schedule: %s
inputs: []
outputs: []
quality:
  min_length: 50
metadata:
  category: %s
  tags: %s
`,
		inst.Definition.Name,
		time.Now().Format(time.RFC3339),
		inst.Definition.Name,
		inst.Definition.Description,
		inst.Definition.Version,
		inst.Definition.Tree,
		inst.Definition.Schedule,
		inst.Definition.Metadata["category"],
		inst.Definition.Metadata["tags"],
	)

	return os.WriteFile(outputPath, []byte(data), 0644)
}

// SkillToAgent generates an agent definition from a Hermes SKILL.md file.
// Uses the existing bt_create_agent MCP tool under the hood.
func (c *Catalog) SkillToAgent(skillPath string) (*Definition, error) {
	skillData, err := os.ReadFile(skillPath)
	if err != nil {
		return nil, fmt.Errorf("read skill: %w", err)
	}

	// Extract skill metadata
	skillName := extractYAMLField(string(skillData), "name")
	skillDesc := extractYAMLField(string(skillData), "description")

	if skillName == "" {
		// Try filename
		skillName = filepath.Base(skillPath)
		skillName = skillName[:len(skillName)-len(filepath.Ext(skillName))]
	}

	// Determine best tree for this skill based on keywords
	tree := inferTree(string(skillData))

	def := &Definition{
		Name:        skillName,
		Description: fmt.Sprintf("Auto-generated from skill: %s", skillDesc),
		Version:     "1.0.0",
		Tree:        tree,
		Schedule:    "on_demand",
		Metadata: map[string]string{
			"category":  "skill-generated",
			"tags":      extractYAMLField(string(skillData), "tags"),
			"skill_src": skillPath,
		},
	}

	return def, nil
}

// inferTree determines the best behavior tree for a skill based on its content.
func inferTree(content string) string {
	lower := strings.ToLower(content)
	switch {
	case strings.Contains(lower, "code review") || strings.Contains(lower, "pr review"):
		return "domain:code_review"
	case strings.Contains(lower, "research") || strings.Contains(lower, "investigate"):
		return "research:deep_research"
	case strings.Contains(lower, "monitor") || strings.Contains(lower, "health"):
		return "domain:agent_monitor"
	case strings.Contains(lower, "pipeline") || strings.Contains(lower, "etl"):
		return "domain:data_pipeline"
	case strings.Contains(lower, "meeting") || strings.Contains(lower, "transcript"):
		return "domain:meeting_notes"
	case strings.Contains(lower, "financ") || strings.Contains(lower, "trading") || strings.Contains(lower, "dollars"):
		return "finance:market_researcher"
	case strings.Contains(lower, "devops") || strings.Contains(lower, "ci/cd"):
		return "domain:devops_ci"
	case strings.Contains(lower, "security") || strings.Contains(lower, "audit"):
		return "domain:security_audit"
	default:
		return "domain:default"
	}
}

func (c *Catalog) matches(entry CatalogEntry, query string) bool {
	if strings.Contains(strings.ToLower(entry.Name), query) {
		return true
	}
	if strings.Contains(strings.ToLower(entry.Description), query) {
		return true
	}
	if strings.Contains(strings.ToLower(entry.Category), query) {
		return true
	}
	for _, tag := range entry.Tags {
		if strings.Contains(strings.ToLower(tag), query) {
			return true
		}
	}
	return false
}

func (c *Catalog) isInstalled(name string) bool {
	_, err := c.reg.Get(name)
	return err == nil
}

func extractYAMLField(yaml, key string) string {
	// Simple YAML field extractor — finds "key: value" on a single line
	prefix := key + ":"
	for _, line := range strings.Split(yaml, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, prefix) {
			value := strings.TrimSpace(trimmed[len(prefix):])
			// Strip quotes
			if len(value) >= 2 && value[0] == '"' && value[len(value)-1] == '"' {
				value = value[1 : len(value)-1]
			}
			return value
		}
	}
	return ""
}

func splitTags(tagStr string) []string {
	if tagStr == "" {
		return nil
	}
	tags := strings.Split(tagStr, ",")
	for i := range tags {
		tags[i] = strings.TrimSpace(tags[i])
	}
	return tags
}
