package goap

import "testing"

func TestAgentPublishesObservedStateBeforeCallbackAndDoesNotInventEffects(t *testing.T) {
	for _, complete := range []bool{false, true} {
		calls := 0
		action := NewAction("write_report", 1, WorldState{"ready": true}, WorldState{"done": true})
		agent := NewAgent(DefaultPlanner([]Action{action}), ActionRegistry{
			"write_report": func(state WorldState) (WorldState, error) {
				calls++
				delete(state, "stale")
				state["observed_count"] = 3
				if complete {
					state["done"] = true
				}
				return state, nil
			},
		})
		agent.SetState("ready", true)
		agent.SetState("stale", "remove")
		agent.SetGoals(NewGoal("report", 1, WorldState{"done": true}))
		callback := false
		agent.Callbacks.OnStepComplete = func(_ int, _ *Action, err error) {
			callback = true
			if (err == nil) != complete {
				t.Errorf("unverified step credited: complete=%v err=%v", complete, err)
			}
			if _, ok := agent.GetState("stale"); ok {
				t.Error("deleted fact resurrected in live state")
			}
			if count, _ := agent.GetState("observed_count"); count != 3 {
				t.Errorf("callback did not see executor observation: %v", count)
			}
		}
		run := agent.Run()
		if !callback || calls != 1 || (run.Status == AgentSucceeded) != complete {
			t.Fatalf("complete=%v calls=%d callback=%v run=%+v", complete, calls, callback, run)
		}
		if value, ok := agent.GetState("done"); !complete && (ok || value != nil) {
			t.Fatalf("planned effect invented: %v", value)
		}
	}
}
