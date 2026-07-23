package benchmark

import "github.com/nico/go-bt-evolve/internal/engine"

// detectPath returns the strategy path that was actually executed by the tree.
// Priority: 1) bb.CurrentPath (set by tree traversal during execution)
//  2. First entry in bb.VisitedPaths
//  3. Keyword-based fallback on bb.Task (backward compatibility)
func detectPath(_ string, bb *engine.Blackboard) string {
	// PRIMARY: tree-internal path tracking — reflects what the tree actually did
	if bb.CurrentPath != "" {
		return bb.CurrentPath
	}
	if len(bb.VisitedPaths) > 0 {
		return bb.VisitedPaths[0]
	}

	// FALLBACK: keyword matching on task description (backward compat only)
	task := bb.Task
	switch {
	// Cron domain must be matched BEFORE the generic HealthPath case below
	// (which captures the bare "capacity planning" keyword), otherwise
	// cron-specific capacity-planning tasks silently fall through to
	// HealthPath and never reach CronPath — the ExpectedPath the eval suites
	// declare for the whole Cron() domain. Keywords are deliberately
	// "cron "-prefixed so a generic (non-cron) capacity-planning task stays
	// HealthPath.
	case containsStr(task, "cron job"), containsStr(task, "cron audit"), containsStr(task, "cron capacity"),
		containsStr(task, "cron governance"):
		return "CronPath"
	case containsStr(task, "health"), containsStr(task, "agent status"), containsStr(task, "disk usage"),
		containsStr(task, "capacity planning"), containsStr(task, "sre"), containsStr(task, "sla"),
		containsStr(task, "chaos"):
		return "HealthPath"
	case containsStr(task, "meeting"), containsStr(task, "transcribe"), containsStr(task, "standup"),
		containsStr(task, "minutes"), containsStr(task, "diarize"):
		return "MeetingPath"
	case containsStr(task, "tree fitness"), containsStr(task, "mutation candidate"), containsStr(task, "evolution safety"),
		containsStr(task, "ensemble evolution"), containsStr(task, "multi-objective evolution"), containsStr(task, "fleet-wide"):
		return "EvolutionPath"
	case containsStr(task, "platform maturity"), containsStr(task, "lowest-scoring"), containsStr(task, "gap analysis"),
		containsStr(task, "comparative maturity"), containsStr(task, "maturity trend"), containsStr(task, "production readiness"):
		return "PlatformEvalPath"
	case containsStr(task, "notebooklm"), containsStr(task, "chat quer"), containsStr(task, "briefing doc"),
		containsStr(task, "mind map"), containsStr(task, "cross-notebook"), containsStr(task, "research pipeline"):
		return "NotebookLMPath"
	case containsStr(task, "vault"), containsStr(task, "ingest the session"), containsStr(task, "synthesize daily"),
		containsStr(task, "cross-link"), containsStr(task, "weekly sweep"), containsStr(task, "knowledge gap"):
		return "VaultPath"
	case containsStr(task, "dcf"), containsStr(task, "lbo"), containsStr(task, "valuation"), containsStr(task, "earnings"),
		containsStr(task, "kyc"), containsStr(task, "financial"):
		return "FinancePath"
	// Data-engineering domain must be matched BEFORE the generic DevOps case below
	// (which captures the bare "pipeline" keyword), otherwise unmistakable ETL /
	// data-lake / data-mesh tasks silently fall through to DevOpsPath/BuildPath/
	// GeneralPath and never reach DataPipelinePath — the ExpectedPath the eval suites
	// declare for the whole DataPipeline() domain. Keywords are deliberately
	// data-specific so a generic CI/CD "pipeline" or "deploy" task stays DevOpsPath.
	case containsStr(task, "etl"), containsStr(task, "parquet"), containsStr(task, "data lake"),
		containsStr(task, "data mesh"), containsStr(task, "data contract"), containsStr(task, "data quality"),
		containsStr(task, "data product"), containsStr(task, "streaming ingest"),
		containsStr(task, "schema validation"), containsStr(task, "schema evolution"),
		containsStr(task, "exactly-once"), containsStr(task, "incremental load"),
		containsStr(task, "lineage"):
		return "DataPipelinePath"
	case containsStr(task, "deploy"), containsStr(task, "pipeline"), containsStr(task, "docker"),
		containsStr(task, "kubernetes"), containsStr(task, "devops"):
		return "DevOpsPath"
	case containsStr(task, "research"), containsStr(task, "investigate"), containsStr(task, "paper"),
		containsStr(task, "literature"), containsStr(task, "deep dive"):
		return "ResearchPath"
	case containsStr(task, "analyze"), containsStr(task, "strategy"), containsStr(task, "forecast"):
		return "ThinkTankPath"
	case containsStr(task, "refactor"), containsStr(task, "restructure"), containsStr(task, "clean up"):
		return "RefactoringPath"
	case containsStr(task, "what "), containsStr(task, "how "), containsStr(task, "why "), containsStr(task, "explain"):
		return "KnowledgePath"
	case containsStr(task, "kanban"), containsStr(task, "card"), containsStr(task, "board"), containsStr(task, "sprint"):
		return "WorkflowPath"
	case containsStr(task, "crash"), containsStr(task, "incident"), containsStr(task, "outage"), containsStr(task, "postmortem"):
		return "IncidentPath"
	// Security-audit domain must be matched BEFORE the generic code-review case
	// below (which captures bare "audit"/"security "), otherwise unmistakable
	// security-audit tasks silently fall through to CodeReviewPath/GeneralPath and
	// never reach SecurityPath — the ExpectedPath the eval suites declare for them.
	// Keywords are deliberately specific so a generic "review this code for security
	// bugs" stays CodeReviewPath.
	case containsStr(task, "sql injection"), containsStr(task, "hardcoded credential"),
		containsStr(task, "penetration test"), containsStr(task, "threat model"),
		containsStr(task, "vulnerabilit"), containsStr(task, "owasp"), containsStr(task, "cvss"),
		containsStr(task, "privilege escalation"), containsStr(task, "attack surface"),
		containsStr(task, "stride"), containsStr(task, "ssrf"), containsStr(task, "security audit"),
		containsStr(task, "security review"):
		return "SecurityPath"
	case containsStr(task, "review"), containsStr(task, "bug"), containsStr(task, "security "), containsStr(task, "audit"):
		return "CodeReviewPath"
	case containsStr(task, "build"), containsStr(task, "compile"), containsStr(task, "go test"):
		return "BuildPath"
	}
	return "GeneralPath"
}
