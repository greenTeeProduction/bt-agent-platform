package engine

// NotifyFleetEventFn publishes an operator-facing fleet lifecycle
// notification — landings and PR state changes — onto the agent event bus
// (wired from cmd/bt-agent as a task_complete AgentEvent, which rides the
// existing bt-task-complete webhook → Hermes → Telegram path). These events
// exist because the interesting moments happen MID-cycle: the scheduler's
// own completion notification arrives only when the whole cycle ends (hours
// later behind a long implementation run) and a landing cycle's final
// outcome is often routine no_change, which the notification throttle then
// suppresses — on 2026-07-22 every PR open/merge and landing of the day was
// invisible on Telegram. Nil when unwired (tests, CLI siblings): call
// notifyFleetEvent, which nil-checks. Outcomes here are never "no_change",
// so the routine throttle cannot swallow them.
var NotifyFleetEventFn func(source, outcome, summary string)

func notifyFleetEvent(source, outcome, summary string) {
	if NotifyFleetEventFn == nil {
		return
	}
	NotifyFleetEventFn(source, outcome, summary)
}
