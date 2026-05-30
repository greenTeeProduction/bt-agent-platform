package domains

import "github.com/nico/go-bt-evolve/internal/evolution"

// AlertRouterTree builds a minimal alert-routing behavior tree.
// Routes any alert (disk, security, trading, health, incident) by severity
// and type. No LLM calls — keyword-matching only, instant execution.
// Conditions and actions registered in engine/alert_registry.go init().
func AlertRouterTree() *evolution.SerializableNode {
	return &evolution.SerializableNode{
		Type: "Sequence",
		Name: "AlertRouter_Main",
		Children: []evolution.SerializableNode{
			seq("PreGate",
				cond("ValidateInput", "Non-empty"),
				cond("HasClearTask", "Task has valid content"),
			),
			sel("StrategyRouter",
				seq("CriticalAlert",
					cond("IsCritical", "Detect critical/emergency/urgent keywords"),
					act("RouteToAllChannels", "Route to all connected channels"),
				),
				seq("SecurityAlert",
					cond("IsSecurity", "Detect security/breach/attack/intrusion keywords"),
					act("RouteToSecurityChannel", "Route to security team"),
				),
				seq("TradingAlert",
					cond("IsTrading", "Detect trading/BTC/price/signal keywords"),
					act("RouteToTradingChannel", "Route to trading channels"),
				),
				seq("DiskAlert",
					cond("IsDiskAlert", "Detect disk/storage/filesystem keywords"),
					act("RouteToDevOpsChannel", "Route to devops/admin"),
				),
				seq("HealthAlert",
					cond("IsHealthAlert", "Detect health/monitor/down/failure keywords"),
					act("RouteToDevOpsChannel", "Route to devops/admin"),
				),
				seq("GeneralAlert",
					cond("TaskIsNotEmpty", "Any non-empty task"),
					act("RouteToDefaultChannel", "Route to default notification channel"),
				),
			),
			act("MarkSuccessful", "Mark task as successful"),
			act("UpdateBehaviorTree", "Evolve"),
		},
	}
}
