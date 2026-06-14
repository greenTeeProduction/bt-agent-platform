package agent

import "strings"

// pipelineAgentTrees maps workflow-friendly agent names to behavior tree IDs
// when no registry entry exists for that name.
var pipelineAgentTrees = map[string]string{
	"hermes-researcher":    "research:deep_research",
	"daily-researcher":     "research:deep_research",
	"research-agent":       "research:deep_research",
	"quick-researcher":     "research:quick_research",
	"code-reviewer":        "domain:code_review",
	"hermes-code-reviewer": "domain:code_review",
	"bt-implementer":       "godev",
	"refactoring-agent":    "domain:refactoring",
	"system-monitor":       "domain:agent_monitor",
	"devops-agent":         "domain:devops_ci",
	"ci-agent":             "domain:devops_ci",
	"security-auditor":     "domain:security_audit",
	"notebooklm":           "notebooklm-bridge",
	"notebooklm-bridge":    "notebooklm-bridge",
	"data-pipeline-agent":  "domain:data_pipeline",
	"notification-router":  "domain:trading_signal",
	"vault":                "vault_manager",
	"session-indexer":      "notebooklm-consumer",
	"thinktank":            "thinktank:synthesis",
	"default":              "godev",
}

// ResolvePipelineAgent returns the tree ID for a workflow agent name.
func ResolvePipelineAgent(agentName string) string {
	if tid, ok := pipelineAgentTrees[agentName]; ok {
		return tid
	}
	if tid, ok := pipelineAgentTrees[strings.ToLower(agentName)]; ok {
		return tid
	}
	return "godev"
}

// ResolveRunTarget picks the RunOnce agent/tree key for a logical agent name.
func (d *RunDeps) ResolveRunTarget(agentName string) string {
	if d != nil && d.Registry != nil {
		if _, err := d.Registry.Get(agentName); err == nil {
			return agentName
		}
	}
	return ResolvePipelineAgent(agentName)
}
