package a2a

import (
	"fmt"
	"strings"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/nico/go-bt-evolve/internal/agent"
)

// ConvertToAgentCard creates an A2A AgentCard from a BT agent Definition.
// The card includes a JSON-RPC interface at the given base URL and one skill
// derived from the agent's tree type.
func ConvertToAgentCard(def agent.Definition, baseURL string) (*a2a.AgentCard, error) {
	if def.Name == "" {
		return nil, fmt.Errorf("agent name is required")
	}

	url := strings.TrimRight(baseURL, "/") + "/agents/" + def.Name

	card := &a2a.AgentCard{
		Name:        def.Name,
		Description: def.Description,
		Version:     def.Version,
		Skills: []a2a.AgentSkill{{
			ID:          def.Tree,
			Name:        treeSkillName(def.Tree),
			Description: def.Description,
			Tags:        treeTags(def.Tree),
		}},
		DefaultInputModes:  []string{"text/plain", "application/json"},
		DefaultOutputModes: []string{"text/plain", "application/json", "text/markdown"},
		SupportedInterfaces: []*a2a.AgentInterface{
			a2a.NewAgentInterface(url, a2a.TransportProtocolJSONRPC),
		},
	}

	return card, nil
}

// treeSkillName derives a human-readable skill name from a tree ID.
// "domain:code_review" → "code review (domain)"
// "research:deep_research" → "deep research (research)"
func treeSkillName(treeID string) string {
	parts := strings.SplitN(treeID, ":", 2)
	if len(parts) == 2 {
		return strings.ReplaceAll(parts[1], "_", " ") + " (" + parts[0] + ")"
	}
	return treeID
}

// treeTags extracts search tags from a tree ID.
// "domain:code_review" → ["domain", "code", "review"]
func treeTags(treeID string) []string {
	parts := strings.SplitN(treeID, ":", 2)
	tags := []string{parts[0]}
	if len(parts) == 2 {
		tags = append(tags, strings.Split(parts[1], "_")...)
	}
	return tags
}

// BuildCardRegistry generates AgentCards for all agents in the registry.
func BuildCardRegistry(reg *agent.Registry, baseURL string) (map[string]*a2a.AgentCard, error) {
	cards := make(map[string]*a2a.AgentCard)
	for _, inst := range reg.List() {
		card, err := ConvertToAgentCard(inst.Definition, baseURL)
		if err != nil {
			continue // skip agents with invalid definitions
		}
		cards[inst.Definition.Name] = card
	}
	return cards, nil
}
