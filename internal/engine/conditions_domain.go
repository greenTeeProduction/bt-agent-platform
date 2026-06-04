package engine

import (
	"github.com/nico/go-bt-evolve/internal/util"
	"strings"
)

func init() {
	RegisterCondition("IsStudioTask", func(bb *Blackboard) bool {
		return util.ContainsAnyStr(strings.ToLower(bb.Task), "podcast", "briefing", "faq", "audio", "timeline", "create", "studio")
	})
	RegisterCondition("IsResearchTask", func(bb *Blackboard) bool {
		return util.ContainsAnyStr(strings.ToLower(bb.Task), "research", "web search", "discover", "find sources", "deep research")
	})
	RegisterCondition("IsKanbanTask", func(bb *Blackboard) bool {
		return util.ContainsAnyStr(strings.ToLower(bb.Task), "card", "kanban", "board", "focalboard", "column", "backlog", "todo", "in progress", "sprint", "status", "move", "assign")
	})
	RegisterCondition("IsBoardCheck", func(bb *Blackboard) bool {
		return util.ContainsAnyStr(bb.Task, "scan", "check", "monitor", "stale", "board", "status", "bottleneck")
	})
	RegisterCondition("NeedsDispatch", func(bb *Blackboard) bool {
		return util.ContainsAnyStr(bb.Task, "dispatch", "assign", "next", "start", "pick up")
	})
	RegisterCondition("IsStandup", func(bb *Blackboard) bool {
		return util.ContainsAnyStr(bb.Task, "standup", "daily", "status", "report")
	})
	RegisterCondition("IsCreateTask", func(bb *Blackboard) bool {
		return util.ContainsAnyStr(bb.Task, "create", "new card", "add card", "backlog")
	})
	RegisterCondition("IsRefinement", func(bb *Blackboard) bool {
		return util.ContainsAnyStr(bb.Task, "refine", "expand", "detail", "planning")
	})
	RegisterCondition("IsQA", func(bb *Blackboard) bool {
		return util.ContainsAnyStr(bb.Task, "qa", "test", "validate", "verify", "check")
	})
	RegisterCondition("IsSessionStart", func(bb *Blackboard) bool {
		return util.ContainsAnyStr(bb.Task, "session start", "boot", "wake", "startup", "begin", "morning")
	})
	RegisterCondition("HasNewContent", func(bb *Blackboard) bool {
		return util.ContainsAnyStr(bb.Task, "ingest", "import", "new content", "source", "transcript", "article", "save", "raw")
	})
	RegisterCondition("NeedsSynthesis", func(bb *Blackboard) bool {
		return util.ContainsAnyStr(bb.Task, "synthesize", "wiki", "extract", "create note", "knowledge", "concept")
	})
	RegisterCondition("NeedsCrossLinks", func(bb *Blackboard) bool {
		return util.ContainsAnyStr(bb.Task, "link", "cross-link", "audit", "connect", "orphan")
	})
	RegisterCondition("NeedsIndexUpdate", func(bb *Blackboard) bool {
		return util.ContainsAnyStr(bb.Task, "index", "update", "refresh", "MOC")
	})
	RegisterCondition("IsSessionEnd", func(bb *Blackboard) bool {
		return util.ContainsAnyStr(bb.Task, "session end", "wrap", "close", "daily summary", "end of day", "goodbye")
	})
	RegisterCondition("HasCachedFitness", func(bb *Blackboard) bool {
		if bb.ChainState != nil {
			_, ok := bb.ChainState["cached_fitness"]
			return ok
		}
		return false
	})
	RegisterCondition("HasFitnessImproved", func(bb *Blackboard) bool {
		if bb.ChainState != nil {
			current, _ := bb.ChainState["current_fitness"].(float64)
			best, _ := bb.ChainState["best_fitness"].(float64)
			return current > best
		}
		return false
	})
	RegisterCondition("IsResearchQuery", func(bb *Blackboard) bool {
		lower := strings.ToLower(bb.Task)
		return util.ContainsAnyStr(lower, "research", "investigate", "analyze", "what is", "how does",
			"explain", "compare", "deep dive", "report on", "find out", "look into",
			"literature", "study", "survey", "overview", "landscape",
			"what are", "who", "when", "where", "why", "top ", "best ",
			"most popular", "recommend", "suggest", "tell me about",
			"summarize", "history of", "future of", "trends", "llm",
			"framework", "python", "rust", "golang", "kubernetes",
			"search", "find ", "news", "ai ", "verification", "verify",
			"check ", "latest", "update", "review", "scan", "audit",
			"look up", "lookup", "gather", "collect", "compile", "discover")
	})
	RegisterCondition("IsAmbiguousQuery", func(bb *Blackboard) bool {
		return len(bb.Task) < 15 || util.ContainsAnyStr(bb.Task, "it", "this", "that thing") || !util.ContainsAnyStr(bb.Task, "?", "who", "what", "when", "where", "why", "how")
	})
	RegisterCondition("IsSimpleQuery", func(bb *Blackboard) bool {
		return len(bb.Task) < 60 && !util.ContainsAnyStr(bb.Task, "compare", "versus", "vs", "analysis", "deep", "comprehensive")
	})
	RegisterCondition("IsComparisonQuery", func(bb *Blackboard) bool {
		return util.ContainsAnyStr(bb.Task, "compare", "versus", "vs", "difference between", "contrast")
	})
	RegisterCondition("IsDeepQuery", func(bb *Blackboard) bool {
		return len(bb.Task) > 100 || util.ContainsAnyStr(bb.Task, "comprehensive", "deep dive", "in-depth", "thorough", "full report")
	})
	RegisterCondition("DetectKnowledgeGaps", func(bb *Blackboard) bool {
		return bb.Result == "" || util.ContainsAnyStr(bb.Result, "gap", "missing", "unknown", "unclear", "TODO")
	})
	RegisterCondition("CheckSourceCount", func(bb *Blackboard) bool {
		return len(bb.Result) > 100
	})
	RegisterCondition("CheckCitationFormat", func(bb *Blackboard) bool {
		return util.ContainsAnyStr(bb.Result, "[", "source:", "http")
	})
	RegisterCondition("IsCodeTask", func(bb *Blackboard) bool {
		return util.ContainsAnyStr(bb.Task, "code", "function", "bug", "fix", "refactor")
	})
	RegisterCondition("IsBugCheck", func(bb *Blackboard) bool {
		return util.ContainsAnyStr(bb.Task, "bug", "fix", "error", "crash", "null", "race")
	})
	RegisterCondition("IsSecurityCheck", func(bb *Blackboard) bool {
		return util.ContainsAnyStr(strings.ToLower(bb.Task), "security", "exploit", "vulnerability", "penetration", "auth", "audit", "xss", "sql injection", "csrf", "owasp", "injection")
	})
	RegisterCondition("IsStyleCheck", func(bb *Blackboard) bool {
		return util.ContainsAnyStr(bb.Task, "style", "lint", "format", "naming", "clean")
	})
	RegisterCondition("IsCIBuildTask", func(bb *Blackboard) bool {
		return util.ContainsAnyStr(bb.Task, "build", "deploy", "ci", "cd", "pipeline", "release")
	})
	RegisterCondition("NeedsBuild", func(bb *Blackboard) bool {
		return util.ContainsAnyStr(bb.Task, "build", "compile")
	})
	RegisterCondition("NeedsTestRun", func(bb *Blackboard) bool {
		return util.ContainsAnyStr(bb.Task, "test", "run tests")
	})
	RegisterCondition("NeedsLinting", func(bb *Blackboard) bool {
		return util.ContainsAnyStr(bb.Task, "lint", "static")
	})
	RegisterCondition("NeedsDeploy", func(bb *Blackboard) bool {
		return util.ContainsAnyStr(bb.Task, "deploy", "release", "ship")
	})
	RegisterCondition("IsMonitorTask", func(bb *Blackboard) bool {
		return util.ContainsAnyStr(bb.Task, "monitor", "health", "status", "agent", "watch")
	})
	RegisterCondition("HasDeadAgents", func(bb *Blackboard) bool {
		return util.ContainsAnyStr(bb.Result, "dead", "offline", "unreachable")
	})
	RegisterCondition("PersistentFailures", func(bb *Blackboard) bool {
		return util.ContainsAnyStr(bb.Result, "failed", "3+", "persistent")
	})
	RegisterCondition("IsMetricsRequest", func(bb *Blackboard) bool {
		return util.ContainsAnyStr(bb.Task, "metrics", "stats", "report")
	})
	RegisterCondition("IsRestartRequest", func(bb *Blackboard) bool {
		return util.ContainsAnyStr(bb.Task, "restart", "dead", "down", "revive", "resurrect")
	})
	RegisterCondition("IsRefactorTask", func(bb *Blackboard) bool {
		return util.ContainsAnyStr(bb.Task, "refactor", "improve", "clean", "rewrite")
	})
	RegisterCondition("IsSmellCheck", func(bb *Blackboard) bool {
		return util.ContainsAnyStr(bb.Task, "smell", "cruft", "duplicate", "long")
	})
	RegisterCondition("IsPatternRequest", func(bb *Blackboard) bool {
		return util.ContainsAnyStr(bb.Task, "pattern", "design", "architecture")
	})
	RegisterCondition("NeedsVerification", func(bb *Blackboard) bool {
		return util.ContainsAnyStr(bb.Task, "verify", "test", "check")
	})
	RegisterCondition("IsSecurityTask", func(bb *Blackboard) bool {
		return util.ContainsAnyStr(bb.Task, "security", "audit", "threat", "vulnerability")
	})
	RegisterCondition("IsSASTRequest", func(bb *Blackboard) bool {
		return util.ContainsAnyStr(bb.Task, "sast", "static analysis")
	})
	RegisterCondition("IsDepScanRequest", func(bb *Blackboard) bool {
		return util.ContainsAnyStr(bb.Task, "dependency", "package", "cve", "library")
	})
	RegisterCondition("IsSecretScan", func(bb *Blackboard) bool {
		return util.ContainsAnyStr(bb.Task, "secret", "credential", "key", "token", "password")
	})
	RegisterCondition("IsThreatModel", func(bb *Blackboard) bool {
		return util.ContainsAnyStr(bb.Task, "threat", "model", "attack", "stride")
	})
	RegisterCondition("IsExtractRequest", func(bb *Blackboard) bool {
		return util.ContainsAnyStr(bb.Task, "extract", "ingest", "load")
	})
	RegisterCondition("IsTransformRequest", func(bb *Blackboard) bool {
		return util.ContainsAnyStr(bb.Task, "transform", "clean", "normalize")
	})
	RegisterCondition("IsLoadRequest", func(bb *Blackboard) bool {
		return util.ContainsAnyStr(bb.Task, "load", "write", "store")
	})
	RegisterCondition("HasTranscript", func(bb *Blackboard) bool {
		return len(bb.Task) > 200
	})
	RegisterCondition("IsActionExtraction", func(bb *Blackboard) bool {
		return util.ContainsAnyStr(bb.Task, "action", "todo", "next")
	})
	RegisterCondition("IsSummaryRequest", func(bb *Blackboard) bool {
		return util.ContainsAnyStr(bb.Task, "summary", "notes", "minutes")
	})
	RegisterCondition("IsFollowUp", func(bb *Blackboard) bool {
		return util.ContainsAnyStr(bb.Task, "follow", "reminder")
	})
	RegisterCondition("IsCrashTask", func(bb *Blackboard) bool {
		return util.ContainsAnyStr(bb.Task, "crash", "error", "stack", "panic", "trace")
	})
	RegisterCondition("HasStackTrace", func(bb *Blackboard) bool {
		return util.ContainsAnyStr(bb.Task, "at ", ".go:", ".rs:", "goroutine", "thread")
	})
	RegisterCondition("IsRootCauseRequest", func(bb *Blackboard) bool {
		return util.ContainsAnyStr(bb.Task, "root cause", "why", "debug")
	})
	RegisterCondition("HasProposedFix", func(bb *Blackboard) bool {
		return util.ContainsAnyStr(bb.Result, "fix", "patch", "change")
	})
	RegisterCondition("IsPreventionRequest", func(bb *Blackboard) bool {
		return util.ContainsAnyStr(bb.Task, "prevent", "harden", "guard")
	})
	RegisterCondition("IsGameTask", func(bb *Blackboard) bool {
		return util.ContainsAnyStr(bb.Task, "game", "npc", "ai", "behavior")
	})
	RegisterCondition("IsPatrolState", func(bb *Blackboard) bool {
		return util.ContainsAnyStr(bb.Task, "patrol", "idle", "wander")
	})
	RegisterCondition("IsDetectState", func(bb *Blackboard) bool {
		return util.ContainsAnyStr(bb.Task, "detect", "spot", "see", "hear")
	})
	RegisterCondition("IsChaseState", func(bb *Blackboard) bool {
		return util.ContainsAnyStr(bb.Task, "chase", "pursue", "follow")
	})
	RegisterCondition("IsCombatState", func(bb *Blackboard) bool {
		return util.ContainsAnyStr(bb.Task, "attack", "fight", "combat", "shoot")
	})
	RegisterCondition("IsRetreatState", func(bb *Blackboard) bool {
		return util.ContainsAnyStr(bb.Task, "retreat", "flee", "escape", "heal")
	})
	RegisterCondition("IsTradingTask", func(bb *Blackboard) bool {
		return util.ContainsAnyStr(bb.Task, "trading", "signal", "market", "price", "stock", "alert", "critical", "incident", "notify", "route", "severity", "disk", "security", "health")
	})
	RegisterCondition("IsDataRequest", func(bb *Blackboard) bool {
		return util.ContainsAnyStr(bb.Task, "data", "fetch", "pull", "price")
	})
	RegisterCondition("IsTAPath", func(bb *Blackboard) bool {
		return util.ContainsAnyStr(bb.Task, "technical", "indicator", "pattern", "rsi", "macd", "sma")
	})
	RegisterCondition("IsSignalRequest", func(bb *Blackboard) bool {
		return util.ContainsAnyStr(bb.Task, "signal", "buy", "sell", "entry")
	})
	RegisterCondition("IsRiskCheck", func(bb *Blackboard) bool {
		return util.ContainsAnyStr(bb.Task, "risk", "stop", "position", "exposure")
	})
	RegisterCondition("IsAssessRequest", func(bb *Blackboard) bool {
		return util.ContainsAnyStr(bb.Task, "assess", "check", "review", "scan", "audit", "track", "measure", "maturity")
	})
	RegisterCondition("IsSyncRequest", func(bb *Blackboard) bool {
		return util.ContainsAnyStr(bb.Task, "sync", "pollinate", "cross", "align", "mismatch")
	})
	RegisterCondition("IsResearchRequest", func(bb *Blackboard) bool {
		return util.ContainsAnyStr(bb.Task, "research", "analyze", "find ", "query", "search", "discover", "evolution")
	})
	RegisterCondition("IsGraphifyRequest", func(bb *Blackboard) bool {
		return util.ContainsAnyStr(bb.Task, "graphify", "graph", "structural", "codebase", "coupling")
	})
	RegisterCondition("IsBuildRequest", func(bb *Blackboard) bool {
		return util.ContainsAnyStr(bb.Task, "build", "compile", "install", "make", "go build")
	})
	RegisterCondition("IsImplementRequest", func(bb *Blackboard) bool {
		return util.ContainsAnyStr(bb.Task, "implement", "plan", "fix", "create", "pending")
	})
}
