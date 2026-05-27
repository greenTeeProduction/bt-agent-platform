package benchmark

import "github.com/nico/go-bt-evolve/internal/engine"

func detectPath(result string, bb *engine.Blackboard) string {
	task := bb.Task
	switch {
	case containsStr(task, "health"), containsStr(task, "agent status"), containsStr(task, "disk usage"),
		containsStr(task, "capacity planning"), containsStr(task, "sre"), containsStr(task, "sla"),
		containsStr(task, "chaos"):
		return "HealthPath"
	case containsStr(task, "meeting"), containsStr(task, "transcribe"), containsStr(task, "standup"),
		containsStr(task, "minutes"), containsStr(task, "diarize"):
		return "MeetingPath"
	case containsStr(task, "cron job"), containsStr(task, "cron audit"), containsStr(task, "cron capacity"),
		containsStr(task, "cron governance"):
		return "CronPath"
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
	case containsStr(task, "review"), containsStr(task, "bug"), containsStr(task, "security "), containsStr(task, "audit"):
		return "CodeReviewPath"
	case containsStr(task, "build"), containsStr(task, "compile"), containsStr(task, "go test"):
		return "BuildPath"
	}
	return "GeneralPath"
}
