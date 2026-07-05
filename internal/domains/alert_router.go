package domains

import "github.com/nico/go-bt-evolve/internal/evolution"

// AlertRouterTree builds a minimal alert-routing behavior tree.
// Routes any alert (disk, security, trading, health, incident) by severity
// and type. No LLM calls — keyword-matching only, instant execution.
// Conditions and actions registered in engine/alert_registry.go init().
func AlertRouterTree() *evolution.SerializableNode {
	return &evolution.SerializableNode{
		Type:        "Sequence",
		Name:        "AlertRouter_Main",
		Description: "Zero-LLM alert router: validate the alert, route it by keyword-matched type to the right channel, and evolve",
		Children: []evolution.SerializableNode{
			seq("PreGate", "Validate the alert task is non-empty with clear content before routing",
				cond("ValidateInput", "Non-empty"),
				cond("HasClearTask", "Task has valid content"),
			),
			sel("StrategyRouter", "Route the alert by keyword-matched type: critical, security, trading, disk, health, or the default notification channel",
				seq("CriticalAlert", "Route critical/emergency alerts to all connected channels",
					cond("IsCritical", "Detect critical/emergency/urgent keywords"),
					act("RouteToAllChannels", "Route to all connected channels"),
				),
				seq("SecurityAlert", "Route security/breach alerts to the security team channel",
					cond("IsSecurity", "Detect security/breach/attack/intrusion keywords"),
					act("RouteToSecurityChannel", "Route to security team"),
				),
				seq("TradingAlert", "Route trading/price alerts to the trading channels",
					cond("IsTrading", "Detect trading/BTC/price/signal keywords"),
					act("RouteToTradingChannel", "Route to trading channels"),
				),
				seq("DiskAlert", "Route disk/storage alerts to the devops/admin channel",
					cond("IsDiskAlert", "Detect disk/storage/filesystem keywords"),
					act("RouteToDevOpsChannel", "Route to devops/admin"),
				),
				seq("HealthAlert", "Route health/monitoring alerts to the devops/admin channel",
					cond("IsHealthAlert", "Detect health/monitor/down/failure keywords"),
					act("RouteToDevOpsChannel", "Route to devops/admin"),
				),
				seq("GeneralAlert", "Route any remaining non-empty alert to the default notification channel",
					cond("TaskIsNotEmpty", "Any non-empty task"),
					act("RouteToDefaultChannel", "Route to default notification channel"),
				),
			),
			act("MarkSuccessful", "Mark task as successful"),
			act("UpdateBehaviorTree", "Evolve"),
		},
	}
}
