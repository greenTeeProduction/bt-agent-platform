package engine

import (
	"github.com/nico/go-bt-evolve/internal/util"
	"strings"
)

func init() {
	RegisterCondition("IsHighPriority", func(bb *Blackboard) bool {
		return util.ContainsAnyStr(bb.Task, "critical", "urgent", "asap")
	})
	RegisterCondition("ValidateOutput", func(bb *Blackboard) bool {
		return validateOutputQuality(bb)
	})
	RegisterCondition("IsGoRelated", func(bb *Blackboard) bool {
		lower := strings.ToLower(bb.Task)
		return util.ContainsAnyStr(lower, "go ", "golang", ".go", "goroutine", "channel", "interface", "struct", "defer", "package ",
			"gin-gonic", "gin ", "go-bt", "gorm", ".mod", "go sum", "go vet", "go build",
			"http.handler", "gorilla", "middleware", "http.request", "http.response",
			"json-rpc", "go module", "godoc", "go fmt", "golint", "staticcheck",
			"null pointer", "memory leak", "race condition", "deadlock", "mutex",
			"fix:", "bug:", "issue:", "refactor:", "engine:", "gardener:", "mcp:")
	})
	RegisterCondition("IsCodeReview", func(bb *Blackboard) bool {
		return util.ContainsAnyStr(bb.Task, "review", "inspect", "lint", "vet", "staticcheck", "code review")
	})
	RegisterCondition("NeedsCompilation", func(bb *Blackboard) bool {
		return util.ContainsAnyStr(bb.Task, "build", "compile", "go build", "go run", "go install")
	})
	RegisterCondition("NeedsTesting", func(bb *Blackboard) bool {
		return util.ContainsAnyStr(bb.Task, "test", "coverage", "benchmark", "go test", "testing")
	})
	RegisterCondition("IsGoQuestion", func(bb *Blackboard) bool {
		return util.ContainsAnyStr(bb.Task, "what is", "how to", "explain", "best practice", "pattern", "idiom", "convention")
	})
	RegisterCondition("IsDevOps", func(bb *Blackboard) bool {
		return util.ContainsAnyStr(strings.ToLower(bb.Task),
			"deploy", "build", "pipeline", "ci/cd", "ci ", "docker",
			"kubernetes", "k8s", "terraform", "ansible", "jenkins",
			"github actions", "gitlab ci", "circleci", "infrastructure", "devops")
	})
	RegisterCondition("IsDataTask", func(bb *Blackboard) bool {
		return util.ContainsAnyStr(strings.ToLower(bb.Task),
			"etl", "pipeline", "data ", "transform", "extract",
			"load", "schema", "dataset", "csv", "parquet", "sql",
			"delegation", "queue", "index", "session", "memory")
	})
	RegisterCondition("IsAnalysisTask", func(bb *Blackboard) bool {
		return util.ContainsAnyStr(strings.ToLower(bb.Task),
			"strategy", "analysis", "analyze", "foresight", "scenario",
			"implications", "forecast", "roadmap", "synthesis", "think tank")
	})
	RegisterCondition("IsRefactoring", func(bb *Blackboard) bool {
		return util.ContainsAnyStr(strings.ToLower(bb.Task),
			"refactor", "restructure", "clean up", "improve",
			"modernize", "migrate", "simplify")
	})
	RegisterCondition("IsQuestion", func(bb *Blackboard) bool {
		return util.ContainsAnyStr(strings.ToLower(bb.Task),
			"what ", "how ", "why ", "explain", "define",
			"difference", "compare", "best practice", "example")
	})
	RegisterCondition("IsIncident", func(bb *Blackboard) bool {
		return util.ContainsAnyStr(strings.ToLower(bb.Task),
			"crash", "error", "timeout", "incident", "outage",
			"down", "broken", "failure", "panic", "oom")
	})
	RegisterCondition("IsHealthCheck", func(bb *Blackboard) bool {
		return util.ContainsAnyStr(strings.ToLower(bb.Task),
			"health", "agent status", "disk usage", "memory", "cpu",
			"dashboard", "system health", "check all",
			"collect system", "verify the dashboard", "capacity planning",
			"sre runbook", "sla dashboard", "chaos engineering",
			"cron status", "scheduler health")
	})
	RegisterCondition("IsMeetingTask", func(bb *Blackboard) bool {
		return util.ContainsAnyStr(strings.ToLower(bb.Task),
			"transcribe", "meeting", "standup", "minutes", "summarize the",
			"architecture review", "sprint planning", "board meeting",
			"action items", "multi-speaker", "facilitation", "diarize")
	})
	RegisterCondition("IsPlatformEval", func(bb *Blackboard) bool {
		return util.ContainsAnyStr(strings.ToLower(bb.Task),
			"platform maturity", "lowest-scoring", "test suite and report",
			"gap analysis", "comparative maturity", "maturity trends",
			"comprehensive audit", "architecture review", "production readiness",
			"platform eval", "dimension", "maturity across all")
	})
	RegisterCondition("IsCronTask", func(bb *Blackboard) bool {
		return util.ContainsAnyStr(strings.ToLower(bb.Task),
			"cron job", "cron audit", "cron capacity", "cron governance",
			"list all cron", "find any cron", "verify all cron",
			"diagnose the hermes", "cron A/B", "self-healing cron")
	})
	RegisterCondition("IsEvolutionTask", func(bb *Blackboard) bool {
		return util.ContainsAnyStr(strings.ToLower(bb.Task),
			"tree fitness", "evolution algorithm", "mutation candidate",
			"evolution safety", "ensemble evolution", "meta-controller",
			"multi-objective evolution", "fleet-wide evolution",
			"self-evolv", "order mutations", "transposition table")
	})
	RegisterCondition("IsNotebookLMTask", func(bb *Blackboard) bool {
		return util.ContainsAnyStr(strings.ToLower(bb.Task),
			"notebooklm", "chat quer", "briefing doc", "mind map",
			"research notebook", "cross-notebook", "deep research",
			"audio overview", "research pipeline", "full pipeline",
			"research impact", "meta-research")
	})
	RegisterCondition("IsVaultTask", func(bb *Blackboard) bool {
		return util.ContainsAnyStr(strings.ToLower(bb.Task),
			"ingest the session", "synthesize daily", "cross-link",
			"vault", "update the index", "weekly sweep", "wiki page",
			"map of content", "frontmatter", "knowledge gap")
	})
	RegisterCondition("IsFinanceTask", func(bb *Blackboard) bool {
		lower := strings.ToLower(bb.Task)
		return util.ContainsAnyStr(lower, "dcf", "lbo", "comps", "valuation", "ebitda", "revenue", "wacc",
			"financial", "equity", "debt", "irr", "moic", "earnings", "quarterly", "10-k", "10-q",
			"pitch", "model", "excel", "portfolio", "investor", "lp", "gp", "kyc", "aml",
			"reconciliation", "reconcile", "ledger", "general ledger", "accrual", "month-end",
			"close", "audit", "statement", "screening")
	})
	RegisterCondition("IsCompsRequest", func(bb *Blackboard) bool {
		return util.ContainsAnyStr(bb.Task, "comps", "comparable", "multiples", "trading comp", "peer")
	})
	RegisterCondition("IsPrecedentsRequest", func(bb *Blackboard) bool {
		return util.ContainsAnyStr(bb.Task, "precedent", "transaction", "m&a comp", "acquisition")
	})
	RegisterCondition("IsLBORequest", func(bb *Blackboard) bool {
		return util.ContainsAnyStr(bb.Task, "lbo", "leveraged buyout", "buyout", "private equity")
	})
	RegisterCondition("IsDCFRequest", func(bb *Blackboard) bool {
		return util.ContainsAnyStr(bb.Task, "dcf", "discounted cash flow", "intrinsic value", "wacc")
	})
	RegisterCondition("IsDeckRequest", func(bb *Blackboard) bool {
		return util.ContainsAnyStr(bb.Task, "deck", "pitch", "presentation", "powerpoint", "slide")
	})
	RegisterCondition("IsEarningsRequest", func(bb *Blackboard) bool {
		return util.ContainsAnyStr(bb.Task, "earnings", "quarterly", "10-q", "10-k", "8-k", "press release", "transcript")
	})
	RegisterCondition("NeedsModelUpdate", func(bb *Blackboard) bool {
		return util.ContainsAnyStr(bb.Task, "update model", "refresh", "revise", "roll forward")
	})
	RegisterCondition("IsNoteRequest", func(bb *Blackboard) bool {
		return util.ContainsAnyStr(bb.Task, "note", "report", "write-up", "draft", "research")
	})
	RegisterCondition("IsIndustryRequest", func(bb *Blackboard) bool {
		return util.ContainsAnyStr(bb.Task, "industry", "sector", "market", "theme", "trend")
	})
	RegisterCondition("IsCompetitiveRequest", func(bb *Blackboard) bool {
		return util.ContainsAnyStr(bb.Task, "competitive", "landscape", "peer", "market share")
	})
	RegisterCondition("IsIdeaRequest", func(bb *Blackboard) bool {
		return util.ContainsAnyStr(bb.Task, "idea", "opportunity", "screen", "shortlist")
	})
	RegisterCondition("Is3StatementRequest", func(bb *Blackboard) bool {
		return util.ContainsAnyStr(bb.Task, "3-statement", "three statement", "operating model", "income statement")
	})
	RegisterCondition("IsMeetingPrep", func(bb *Blackboard) bool {
		return util.ContainsAnyStr(bb.Task, "briefing", "meeting", "client", "prep", "talking points")
	})
	RegisterCondition("IsValuationRequest", func(bb *Blackboard) bool {
		return util.ContainsAnyStr(bb.Task, "valuation", "gp", "lp", "capital account", "nav")
	})
	RegisterCondition("IsGLReconRequest", func(bb *Blackboard) bool {
		return util.ContainsAnyStr(bb.Task, "gl", "general ledger", "reconcil", "break", "sub-ledger")
	})
	RegisterCondition("IsMonthEndRequest", func(bb *Blackboard) bool {
		return util.ContainsAnyStr(bb.Task, "month-end", "close", "accrual", "roll-forward", "variance")
	})
	RegisterCondition("IsAuditRequest", func(bb *Blackboard) bool {
		return util.ContainsAnyStr(bb.Task, "audit", "statement", "verify", "lp", "capital account")
	})
	RegisterCondition("IsKYCRequest", func(bb *Blackboard) bool {
		return util.ContainsAnyStr(bb.Task, "kyc", "aml", "onboarding", "screening", "sanctions", "pep")
	})
	RegisterCondition("ValidateCompanyState", func(bb *Blackboard) bool {
		if bb.ChainState == nil {
			return false
		}
		_, ok := bb.ChainState["company"]
		return ok
	})
	RegisterCondition("IsEngineeringTask", func(bb *Blackboard) bool {
		return util.ContainsAnyStr(strings.ToLower(bb.Task),
			"engineering", "sprint", "feature", "build", "implement",
			"code", "deploy", "architecture", "tech", "developer",
			"sw. eng", "software eng")
	})
	RegisterCondition("IsMarketingTask", func(bb *Blackboard) bool {
		return util.ContainsAnyStr(strings.ToLower(bb.Task),
			"marketing", "content", "seo", "campaign", "growth",
			"community", "brand", "social", "promotion",
			"advertising", "lead gen", "audience")
	})
	RegisterCondition("IsSalesTask", func(bb *Blackboard) bool {
		return util.ContainsAnyStr(strings.ToLower(bb.Task),
			"sales", "deal", "revenue", "pipeline", "lead",
			"closing", "proposal", "demo", "pricing",
			"customer", "prospect", "quota")
	})
	RegisterCondition("IsPeriodicCheck", func(bb *Blackboard) bool {
		// Always trigger — the agent node decides frequency
		return true
	})
	RegisterCondition("HasSkillGaps", func(bb *Blackboard) bool {
		return util.ContainsAnyStr(bb.Task, "skill", "outdated", "missing", "improve skill", "update skill")
	})
	RegisterCondition("HasWorkflowInefficiencies", func(bb *Blackboard) bool {
		return util.ContainsAnyStr(bb.Task, "workflow", "inefficient", "optimize", "redundant", "slow", "pattern")
	})
	RegisterCondition("HasModelToolIssues", func(bb *Blackboard) bool {
		return util.ContainsAnyStr(bb.Task, "model", "tool", "config", "switch", "tune", "provider")
	})
	RegisterCondition("HasFeatureGaps", func(bb *Blackboard) bool {
		// Triggered when test results indicate missing features
		return bb.ChainState != nil && bb.ChainState["has_feature_gaps"] == true
	})
	RegisterCondition("HasLayoutIssues", func(bb *Blackboard) bool {
		return bb.ChainState != nil && bb.ChainState["has_layout_issues"] == true
	})
	RegisterCondition("HasAPIIssues", func(bb *Blackboard) bool {
		return bb.ChainState != nil && bb.ChainState["has_api_issues"] == true
	})
	RegisterCondition("IsDeepResearchDay", func(bb *Blackboard) bool {
		return true // let the cron schedule handle this, always route
	})
	RegisterCondition("IsDailyResearch", func(bb *Blackboard) bool {
		return true // daily fallback
	})
	RegisterCondition("HasNewAlgorithm", func(bb *Blackboard) bool {
		return util.ContainsAnyStr(bb.Task, "implement", "new algorithm", "research", "create")
	})
	RegisterCondition("HasImprovement", func(bb *Blackboard) bool {
		return util.ContainsAnyStr(bb.Task, "improve", "enhance", "optimize", "tune")
	})
	RegisterCondition("NeedsIntegration", func(bb *Blackboard) bool {
		return util.ContainsAnyStr(bb.Task, "integrate", "connect", "pipeline", "wire")
	})
	RegisterCondition("NeedsSweep", func(bb *Blackboard) bool {
		return util.ContainsAnyStr(bb.Task, "sweep", "update notes", "refresh", "maintain")
	})
	RegisterCondition("NeedsAudit", func(bb *Blackboard) bool {
		return util.ContainsAnyStr(bb.Task, "audit", "review", "check", "verify", "assess", "gap")
	})
	RegisterCondition("NeedsPublish", func(bb *Blackboard) bool {
		return util.ContainsAnyStr(bb.Task, "publish", "export", "generate", "report", "slide", "briefing")
	})
	RegisterCondition("IsIngestTask", func(bb *Blackboard) bool {
		lower := strings.ToLower(bb.Task)
		// Do not match bare "notebooklm": query/research tasks often mention the
		// product or target notebook and should not be swallowed by ingestion.
		if util.ContainsAnyStr(lower, "autonomous daily researcher", "deep research", "web search", "discover", "find sources") {
			return false
		}
		return util.ContainsAnyStr(lower, "ingest", "import", "add source", "add sources", "push to notebook", "upload source")
	})
	RegisterCondition("IsQueryTask", func(bb *Blackboard) bool {
		lower := strings.ToLower(bb.Task)
		if util.ContainsAnyStr(lower, "autonomous daily researcher", "deep research", "web search", "discover", "find sources") {
			return false
		}
		return util.ContainsAnyStr(lower, "ask", "query", "question", "what ", "how ", "summarize notebook", "list sources")
	})

	// ─── BT Manager conditions ──────────────────────────────────────────
	RegisterCondition("HasReflectionStore", func(bb *Blackboard) bool {
		return bb.Reflections != nil
	})

	RegisterCondition("IsDegradedAgent", func(bb *Blackboard) bool {
		if bb.Reflections == nil {
			return false
		}
		records, err := bb.Reflections.LoadAll()
		if err != nil || len(records) == 0 {
			return false
		}
		byTree := groupByTreeName(records)
		for _, recs := range byTree {
			sr := successRate(recs)
			cf := consecutiveFailures(recs)
			if sr < 0.7 || cf >= 3 {
				return true
			}
		}
		return false
	})

	RegisterCondition("IsNewAgent", func(bb *Blackboard) bool {
		if bb.Reflections == nil {
			return true // no data = probably new
		}
		records, err := bb.Reflections.LoadAll()
		if err != nil {
			return true
		}
		return len(records) < 5
	})

	RegisterCondition("IsHealthy", func(bb *Blackboard) bool {
		if bb.Reflections == nil {
			return true // no data = assume healthy
		}
		records, err := bb.Reflections.LoadAll()
		if err != nil || len(records) == 0 {
			return true
		}
		byTree := groupByTreeName(records)
		for _, recs := range byTree {
			sr := successRate(recs)
			cf := consecutiveFailures(recs)
			if sr < 0.7 || cf >= 3 {
				return false
			}
		}
		return true
	})
}
