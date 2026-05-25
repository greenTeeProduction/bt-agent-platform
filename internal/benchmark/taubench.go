package benchmark

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/nico/go-bt-evolve/internal/engine"
	"github.com/nico/go-bt-evolve/internal/evolution"
	"github.com/nico/go-bt-evolve/internal/llm"
)

// DefaultTauBenchRepoPath is the cloned τ-bench repository location.
var DefaultTauBenchRepoPath = filepath.Join(
	os.Getenv("HOME"), ".go-bt-benchmarks", "taubench-repo",
)

// TauBenchEntry represents a single τ-bench evaluation scenario.
// Each entry tests a conversational agent's ability to use domain-specific tools
// given a user scenario.
type TauBenchEntry struct {
	ID              string          `json:"id"`
	Domain          string          `json:"domain"` // airline, retail
	Scenario        string          `json:"scenario"` // the user's initial request / reason for calling
	KnownInfo       string          `json:"known_info"` // info the user knows about themselves
	Persona         string          `json:"persona"`
	ExpectedActions []TauBenchAction `json:"expected_actions"` // sequence of tool calls expected
	NlAssertions     []string        `json:"nl_assertions"` // behavioral assertions
	Tools           []TauBenchTool   `json:"tools"` // available domain tools
}

// TauBenchAction is a single expected tool call in the evaluation criteria.
type TauBenchAction struct {
	ActionID  string                 `json:"action_id"`
	Name      string                 `json:"name"`
	Arguments map[string]interface{} `json:"arguments"`
}

// TauBenchTool defines a domain-specific tool available to the agent.
type TauBenchTool struct {
	Name        string           `json:"name"`
	Description string           `json:"description"`
	Parameters  []TauBenchParam   `json:"parameters"`
}

// TauBenchParam defines a single parameter of a tool.
type TauBenchParam struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Description string `json:"description"`
	Required    bool   `json:"required"`
}

// TauBenchMetrics holds aggregate evaluation results for τ-bench scenarios.
type TauBenchMetrics struct {
	TotalScenarios  int                `json:"total_scenarios"`
	GoalAchieved    int                `json:"goal_achieved"`
	ActionAccuracy  float64            `json:"action_accuracy"`
	AvgTurns        float64            `json:"avg_turns"`
	Results         []TauBenchResult   `json:"results"`
}

// TauBenchResult holds per-scenario evaluation outcome.
type TauBenchResult struct {
	EntryID         string   `json:"entry_id"`
	Scenario        string   `json:"scenario"`
	GoalAchieved    bool     `json:"goal_achieved"`
	ActionsMatched  int      `json:"actions_matched"`
	ActionsExpected int      `json:"actions_expected"`
	OutputSnippet   string   `json:"output_snippet"` // first 200 chars of tree output
	Outcome         string   `json:"outcome"`
	MatchedActions  []string `json:"matched_actions"`
	MissedActions   []string `json:"missed_actions"`
}

// tauBenchTaskJSON mirrors the JSON structure of a τ-bench task.
type tauBenchTaskJSON struct {
	ID          string `json:"id"`
	Description struct {
		Purpose string `json:"purpose"`
	} `json:"description"`
	UserScenario struct {
		Persona string `json:"persona"`
		Instructions struct {
			TaskInstructions string `json:"task_instructions"`
			Domain           string `json:"domain"`
			ReasonForCall    string `json:"reason_for_call"`
			KnownInfo        string `json:"known_info"`
		} `json:"instructions"`
	} `json:"user_scenario"`
	EvaluationCriteria struct {
		Actions []struct {
			ActionID  string                 `json:"action_id"`
			Name      string                 `json:"name"`
			Arguments map[string]interface{} `json:"arguments"`
		} `json:"actions"`
		NLAssertions []string `json:"nl_assertions"`
	} `json:"evaluation_criteria"`
}

// LoadTauBenchTasks parses τ-bench tasks.json for a given domain.
// Domain should be "airline" or "retail".
func LoadTauBenchTasks(domain string) ([]TauBenchEntry, error) {
	repoPath := DefaultTauBenchRepoPath
	if envPath := os.Getenv("TAUBENCH_REPO_PATH"); envPath != "" {
		repoPath = envPath
	}

	tasksPath := filepath.Join(repoPath, "data", "tau2", "domains", domain, "tasks.json")
	data, err := os.ReadFile(tasksPath)
	if err != nil {
		return nil, fmt.Errorf("read tau-bench tasks for %s: %w", domain, err)
	}

	var rawTasks []tauBenchTaskJSON
	if err := json.Unmarshal(data, &rawTasks); err != nil {
		return nil, fmt.Errorf("parse tau-bench tasks for %s: %w", domain, err)
	}

	tools := toolsForDomain(domain)
	var entries []TauBenchEntry

	for _, t := range rawTasks {
		// Build scenario from reason_for_call and known_info
		scenario := strings.TrimSpace(t.UserScenario.Instructions.ReasonForCall)
		knownInfo := strings.TrimSpace(t.UserScenario.Instructions.KnownInfo)
		persona := t.UserScenario.Persona

		var expectedActions []TauBenchAction
		for _, a := range t.EvaluationCriteria.Actions {
			expectedActions = append(expectedActions, TauBenchAction{
				ActionID:  a.ActionID,
				Name:      a.Name,
				Arguments: a.Arguments,
			})
		}

		entries = append(entries, TauBenchEntry{
			ID:              t.ID,
			Domain:          domain,
			Scenario:        scenario,
			KnownInfo:       knownInfo,
			Persona:         persona,
			ExpectedActions: expectedActions,
			NlAssertions:     t.EvaluationCriteria.NLAssertions,
			Tools:           tools,
		})
	}

	return entries, nil
}

// toolsForDomain returns the canonical τ-bench tool definitions for a domain.
func toolsForDomain(domain string) []TauBenchTool {
	switch domain {
	case "airline":
		return airlineTools()
	case "retail":
		return retailTools()
	default:
		return nil
	}
}

// airlineTools returns the Airline domain tool definitions (from tau2 real tools).
func airlineTools() []TauBenchTool {
	return []TauBenchTool{
		{
			Name: "book_reservation",
			Description: "Book a reservation. Requires user_id, origin, destination, flight_type, cabin, flights, passengers, payment_methods, total_baggages, nonfree_baggages, insurance.",
			Parameters: []TauBenchParam{
				{Name: "user_id", Type: "string", Description: "The ID of the user", Required: true},
				{Name: "origin", Type: "string", Description: "Origin airport IATA code", Required: true},
				{Name: "destination", Type: "string", Description: "Destination airport IATA code", Required: true},
				{Name: "flight_type", Type: "string", Description: "one_way or round_trip", Required: true},
				{Name: "cabin", Type: "string", Description: "basic_economy, economy, or business", Required: true},
				{Name: "flights", Type: "array", Description: "Flight details with flight_number and date", Required: true},
				{Name: "passengers", Type: "array", Description: "Passenger details", Required: true},
				{Name: "payment_methods", Type: "array", Description: "Payment method details", Required: true},
				{Name: "total_baggages", Type: "integer", Description: "Total number of baggage items", Required: true},
				{Name: "nonfree_baggages", Type: "integer", Description: "Number of non-free baggage items", Required: true},
				{Name: "insurance", Type: "string", Description: "yes or no", Required: true},
			},
		},
		{
			Name: "calculate",
			Description: "Calculate the result of a mathematical expression.",
			Parameters: []TauBenchParam{
				{Name: "expression", Type: "string", Description: "Math expression, e.g. '2 + 2'", Required: true},
			},
		},
		{
			Name: "cancel_reservation",
			Description: "Cancel the whole reservation.",
			Parameters: []TauBenchParam{
				{Name: "reservation_id", Type: "string", Description: "The reservation ID, e.g. 'ZFA04Y'", Required: true},
			},
		},
		{
			Name: "get_reservation_details",
			Description: "Get the details of a reservation.",
			Parameters: []TauBenchParam{
				{Name: "reservation_id", Type: "string", Description: "The reservation ID, e.g. '8JX2WO'", Required: true},
			},
		},
		{
			Name: "get_user_details",
			Description: "Get the details of a user, including their reservations.",
			Parameters: []TauBenchParam{
				{Name: "user_id", Type: "string", Description: "The user ID, e.g. 'sara_doe_496'", Required: true},
			},
		},
		{
			Name: "list_all_airports",
			Description: "Returns a list of all available airports with IATA codes and city names.",
			Parameters: []TauBenchParam{},
		},
		{
			Name: "search_direct_flight",
			Description: "Search for direct flights between two cities on a specific date.",
			Parameters: []TauBenchParam{
				{Name: "origin", Type: "string", Description: "Origin airport IATA code", Required: true},
				{Name: "destination", Type: "string", Description: "Destination airport IATA code", Required: true},
				{Name: "date", Type: "string", Description: "Date in YYYY-MM-DD format", Required: true},
			},
		},
		{
			Name: "search_onestop_flight",
			Description: "Search for one-stop flights between two cities on a specific date.",
			Parameters: []TauBenchParam{
				{Name: "origin", Type: "string", Description: "Origin airport IATA code", Required: true},
				{Name: "destination", Type: "string", Description: "Destination airport IATA code", Required: true},
				{Name: "date", Type: "string", Description: "Date in YYYY-MM-DD format", Required: true},
			},
		},
		{
			Name: "send_certificate",
			Description: "Send a certificate (compensation) to a user.",
			Parameters: []TauBenchParam{
				{Name: "user_id", Type: "string", Description: "The user ID", Required: true},
				{Name: "amount", Type: "integer", Description: "Amount of the certificate", Required: true},
			},
		},
		{
			Name: "transfer_to_human_agents",
			Description: "Transfer the user to a human agent with a summary.",
			Parameters: []TauBenchParam{
				{Name: "summary", Type: "string", Description: "Summary of the user's issue", Required: true},
			},
		},
		{
			Name: "update_reservation_baggages",
			Description: "Update the baggage information of a reservation.",
			Parameters: []TauBenchParam{
				{Name: "reservation_id", Type: "string", Description: "The reservation ID", Required: true},
				{Name: "total_baggages", Type: "integer", Description: "Updated total baggage count", Required: true},
				{Name: "nonfree_baggages", Type: "integer", Description: "Updated non-free baggage count", Required: true},
				{Name: "payment_id", Type: "string", Description: "Payment ID for any price difference", Required: true},
			},
		},
		{
			Name: "update_reservation_flights",
			Description: "Update the flight information of a reservation.",
			Parameters: []TauBenchParam{
				{Name: "reservation_id", Type: "string", Description: "The reservation ID", Required: true},
				{Name: "cabin", Type: "string", Description: "Cabin class", Required: true},
				{Name: "flights", Type: "array", Description: "Array of flight info objects", Required: true},
				{Name: "payment_id", Type: "string", Description: "Payment ID for price difference", Required: true},
			},
		},
		{
			Name: "update_reservation_passengers",
			Description: "Update the passenger information of a reservation.",
			Parameters: []TauBenchParam{
				{Name: "reservation_id", Type: "string", Description: "The reservation ID", Required: true},
				{Name: "passengers", Type: "array", Description: "Array of passenger objects", Required: true},
			},
		},
		{
			Name: "get_flight_status",
			Description: "Get the status of a flight.",
			Parameters: []TauBenchParam{
				{Name: "flight_number", Type: "string", Description: "The flight number", Required: true},
				{Name: "date", Type: "string", Description: "Date in YYYY-MM-DD format", Required: true},
			},
		},
	}
}

// retailTools returns the Retail domain tool definitions (from tau2 real tools).
func retailTools() []TauBenchTool {
	return []TauBenchTool{
		{
			Name: "calculate",
			Description: "Calculate the result of a mathematical expression.",
			Parameters: []TauBenchParam{
				{Name: "expression", Type: "string", Description: "Math expression, e.g. '2 + 2'", Required: true},
			},
		},
		{
			Name: "cancel_pending_order",
			Description: "Cancel a pending order. Requires user confirmation.",
			Parameters: []TauBenchParam{
				{Name: "order_id", Type: "string", Description: "The order ID, e.g. '#W0000000'", Required: true},
				{Name: "reason", Type: "string", Description: "'no longer needed' or 'ordered by mistake'", Required: true},
			},
		},
		{
			Name: "exchange_delivered_order_items",
			Description: "Exchange items in a delivered order for new items of the same product type.",
			Parameters: []TauBenchParam{
				{Name: "order_id", Type: "string", Description: "The order ID", Required: true},
				{Name: "item_ids", Type: "array", Description: "Item IDs to exchange", Required: true},
				{Name: "new_item_ids", Type: "array", Description: "New item IDs to exchange for", Required: true},
				{Name: "payment_method_id", Type: "string", Description: "Payment method for price difference", Required: true},
			},
		},
		{
			Name: "find_user_id_by_name_zip",
			Description: "Find user ID by first name, last name, and zip code.",
			Parameters: []TauBenchParam{
				{Name: "first_name", Type: "string", Description: "First name", Required: true},
				{Name: "last_name", Type: "string", Description: "Last name", Required: true},
				{Name: "zip", Type: "string", Description: "Zip code", Required: true},
			},
		},
		{
			Name: "find_user_id_by_email",
			Description: "Find user ID by email address.",
			Parameters: []TauBenchParam{
				{Name: "email", Type: "string", Description: "User email address", Required: true},
			},
		},
		{
			Name: "get_order_details",
			Description: "Get the status and details of an order.",
			Parameters: []TauBenchParam{
				{Name: "order_id", Type: "string", Description: "The order ID, e.g. '#W0000000'", Required: true},
			},
		},
		{
			Name: "get_product_details",
			Description: "Get the inventory details of a product.",
			Parameters: []TauBenchParam{
				{Name: "product_id", Type: "string", Description: "The product ID", Required: true},
			},
		},
		{
			Name: "get_item_details",
			Description: "Get the inventory details of an item (variant).",
			Parameters: []TauBenchParam{
				{Name: "item_id", Type: "string", Description: "The item/variant ID", Required: true},
			},
		},
		{
			Name: "get_user_details",
			Description: "Get the details of a user, including their orders.",
			Parameters: []TauBenchParam{
				{Name: "user_id", Type: "string", Description: "The user ID", Required: true},
			},
		},
		{
			Name: "list_all_product_types",
			Description: "List the name and product ID of all product types.",
			Parameters: []TauBenchParam{},
		},
		{
			Name: "modify_pending_order_items",
			Description: "Modify items in a pending order (add or remove items).",
			Parameters: []TauBenchParam{
				{Name: "order_id", Type: "string", Description: "The order ID", Required: true},
				{Name: "item_ids", Type: "array", Description: "Item IDs to modify", Required: true},
				{Name: "new_item_ids", Type: "array", Description: "New item IDs", Required: true},
				{Name: "payment_method_id", Type: "string", Description: "Payment method for price difference", Required: true},
			},
		},
		{
			Name: "return_delivered_order_items",
			Description: "Return items in a delivered order for refund.",
			Parameters: []TauBenchParam{
				{Name: "order_id", Type: "string", Description: "The order ID", Required: true},
				{Name: "item_ids", Type: "array", Description: "Item IDs to return", Required: true},
				{Name: "payment_method_id", Type: "string", Description: "Payment method for refund", Required: true},
			},
		},
		{
			Name: "transfer_to_human_agents",
			Description: "Transfer the user to a human agent with a summary.",
			Parameters: []TauBenchParam{
				{Name: "summary", Type: "string", Description: "Summary of the user's issue", Required: true},
			},
		},
		{
			Name: "think",
			Description: "Internal reasoning tool. Use for complex reasoning or caching.",
			Parameters: []TauBenchParam{
				{Name: "thought", Type: "string", Description: "A thought to think about", Required: true},
			},
		},
	}
}

// EvaluateTauBench runs all τ-bench entries against a behavior tree and returns metrics.
// For each entry, the tree's task is set to the scenario description.
// The evaluation tracks: goal achievement (success outcome), action accuracy
// (how many expected tool names appear in the result), and average turns.
func EvaluateTauBench(tree *evolution.SerializableNode, entries []TauBenchEntry, llmClient llm.LLM) *TauBenchMetrics {
	var results []TauBenchResult
	goalAchieved := 0
	totalActionsMatched := 0
	totalActionsExpected := 0
	var totalDuration int64

	for _, entry := range entries {
		// Build the full task description from scenario and known info
		task := buildTauBenchTask(entry)

		bb := &engine.Blackboard{
			Task: task,
			LLM:  llmClient,
		}

		start := time.Now()
		bt := engine.BuildTree(tree, bb)
		output := engine.RunTask(bb, bt)
		duration := time.Since(start).Milliseconds()
		totalDuration += duration

		goalOK := bb.Outcome == "success"
		if goalOK {
			goalAchieved++
		}

		// Check which expected actions are referenced in the output
		matched, missed := matchActions(output, entry.ExpectedActions)
		totalActionsMatched += len(matched)
		totalActionsExpected += len(entry.ExpectedActions)

		// Output snippet (first 200 chars)
		snippet := output
		if len(snippet) > 200 {
			snippet = snippet[:200] + "..."
		}

		results = append(results, TauBenchResult{
			EntryID:         entry.ID,
			Scenario:        entry.Scenario,
			GoalAchieved:    goalOK,
			ActionsMatched:  len(matched),
			ActionsExpected: len(entry.ExpectedActions),
			OutputSnippet:   snippet,
			Outcome:         bb.Outcome,
			MatchedActions:  matched,
			MissedActions:   missed,
		})
	}

	n := len(results)
	if n == 0 {
		return &TauBenchMetrics{Results: results}
	}

	actionAccuracy := 0.0
	if totalActionsExpected > 0 {
		actionAccuracy = float64(totalActionsMatched) / float64(totalActionsExpected)
	}

	avgTurns := 0.0
	if n > 0 {
		avgTurns = float64(totalDuration) / float64(n) / 1000.0 // seconds
	}

	return &TauBenchMetrics{
		TotalScenarios: n,
		GoalAchieved:   goalAchieved,
		ActionAccuracy: actionAccuracy,
		AvgTurns:       avgTurns,
		Results:        results,
	}
}

// buildTauBenchTask combines scenario, known info, and tools into a single task prompt
// suitable for the behavior tree to process.
func buildTauBenchTask(entry TauBenchEntry) string {
	var sb strings.Builder
	sb.WriteString("You are a customer service agent for a ")
	sb.WriteString(entry.Domain)
	sb.WriteString(" company.\n\n")

	sb.WriteString("USER SCENARIO:\n")
	sb.WriteString(entry.Scenario)
	sb.WriteString("\n\n")

	if entry.KnownInfo != "" {
		sb.WriteString("KNOWN INFO:\n")
		sb.WriteString(entry.KnownInfo)
		sb.WriteString("\n\n")
	}

	if entry.Persona != "" {
		sb.WriteString("USER PERSONA: ")
		sb.WriteString(entry.Persona)
		sb.WriteString("\n\n")
	}

	sb.WriteString("AVAILABLE TOOLS:\n")
	for _, tool := range entry.Tools {
		sb.WriteString("- ")
		sb.WriteString(tool.Name)
		sb.WriteString(": ")
		sb.WriteString(tool.Description)
		sb.WriteString("\n")
	}

	return sb.String()
}

// matchActions checks which expected action names appear in the output.
func matchActions(output string, expected []TauBenchAction) (matched []string, missed []string) {
	outputLower := strings.ToLower(output)
	for _, action := range expected {
		if strings.Contains(outputLower, strings.ToLower(action.Name)) {
			matched = append(matched, action.Name)
		} else {
			missed = append(missed, action.Name)
		}
	}
	return matched, missed
}

// BuiltinTauBenchAirline returns 5 representative airline scenarios curated from τ-bench.
// These cover: flight booking, cancellation, modification, status check, and refund/compensation.
func BuiltinTauBenchAirline() []TauBenchEntry {
	tools := airlineTools()
	return []TauBenchEntry{
		{
			ID:       "airline-builtin-0",
			Domain:   "airline",
			Scenario: "You want to book a flight from San Francisco (SFO) to New York (JFK) for 2 passengers on May 20, 2024 in economy class. You want a direct flight if possible.",
			KnownInfo: "You are Sara Doe. Your user ID is sara_doe_496.",
			ExpectedActions: []TauBenchAction{
				{ActionID: "0_0", Name: "get_user_details", Arguments: map[string]interface{}{"user_id": "sara_doe_496"}},
				{ActionID: "0_1", Name: "search_direct_flight", Arguments: map[string]interface{}{"origin": "SFO", "destination": "JFK", "date": "2024-05-20"}},
				{ActionID: "0_2", Name: "book_reservation", Arguments: nil},
			},
			NlAssertions: []string{"Agent should find flights before booking"},
			Tools:       tools,
		},
		{
			ID:       "airline-builtin-1",
			Domain:   "airline",
			Scenario: "You want to cancel reservation EHGLP3. You were out of town and couldn't cancel within 24 hours.",
			KnownInfo: "You are Emma Kim. Your user ID is emma_kim_9957.",
			ExpectedActions: []TauBenchAction{
				{ActionID: "1_0", Name: "get_user_details", Arguments: map[string]interface{}{"user_id": "emma_kim_9957"}},
				{ActionID: "1_1", Name: "get_reservation_details", Arguments: map[string]interface{}{"reservation_id": "EHGLP3"}},
				{ActionID: "1_2", Name: "cancel_reservation", Arguments: map[string]interface{}{"reservation_id": "EHGLP3"}},
			},
			NlAssertions: []string{"Agent should check cancellation policy before proceeding"},
			Tools:       tools,
		},
		{
			ID:       "airline-builtin-2",
			Domain:   "airline",
			Scenario: "You want to change the flight date for reservation ZFA04Y from May 15 to May 18, 2024, same route, economy class.",
			KnownInfo: "You are John Smith. Your user ID is john_smith_1234. Your reservation is ZFA04Y.",
			ExpectedActions: []TauBenchAction{
				{ActionID: "2_0", Name: "get_reservation_details", Arguments: map[string]interface{}{"reservation_id": "ZFA04Y"}},
				{ActionID: "2_1", Name: "search_direct_flight", Arguments: nil},
				{ActionID: "2_2", Name: "update_reservation_flights", Arguments: nil},
			},
			NlAssertions: []string{"Agent should verify flight availability before modifying"},
			Tools:       tools,
		},
		{
			ID:       "airline-builtin-3",
			Domain:   "airline",
			Scenario: "You want to check the status of flight AA123 on May 15, 2024. You're worried it might be delayed.",
			KnownInfo: "You are Mike Jones. Your user ID is mike_jones_5678.",
			ExpectedActions: []TauBenchAction{
				{ActionID: "3_0", Name: "get_flight_status", Arguments: map[string]interface{}{"flight_number": "AA123", "date": "2024-05-15"}},
			},
			NlAssertions: []string{"Agent should provide clear flight status information"},
			Tools:       tools,
		},
		{
			ID:       "airline-builtin-4",
			Domain:   "airline",
			Scenario: "Your flight in reservation 4OG6T3 was delayed by 4 hours. You want compensation for the inconvenience.",
			KnownInfo: "You are Noah Muller. Your user ID is noah_muller_9847.",
			ExpectedActions: []TauBenchAction{
				{ActionID: "4_0", Name: "get_user_details", Arguments: map[string]interface{}{"user_id": "noah_muller_9847"}},
				{ActionID: "4_1", Name: "get_reservation_details", Arguments: map[string]interface{}{"reservation_id": "4OG6T3"}},
				{ActionID: "4_2", Name: "send_certificate", Arguments: nil},
			},
			NlAssertions: []string{"Agent should verify the delay before offering compensation"},
			Tools:       tools,
		},
	}
}

// BuiltinTauBenchRetail returns 5 representative retail scenarios curated from τ-bench.
// These cover: order lookup, return, exchange, price match, and shipping inquiry.
func BuiltinTauBenchRetail() []TauBenchEntry {
	tools := retailTools()
	return []TauBenchEntry{
		{
			ID:       "retail-builtin-0",
			Domain:   "retail",
			Scenario: "You received order #W2378156 and want to check its status. You want to know when it was delivered.",
			KnownInfo: "You are Yusuf Rossi in zip code 19122.",
			ExpectedActions: []TauBenchAction{
				{ActionID: "0_0", Name: "find_user_id_by_name_zip", Arguments: map[string]interface{}{"first_name": "Yusuf", "last_name": "Rossi", "zip": "19122"}},
				{ActionID: "0_1", Name: "get_order_details", Arguments: map[string]interface{}{"order_id": "#W2378156"}},
			},
			NlAssertions: []string{"Agent should look up the user before checking the order"},
			Tools:       tools,
		},
		{
			ID:       "retail-builtin-1",
			Domain:   "retail",
			Scenario: "You want to return the mechanical keyboard from your delivered order #W2378156. You bought the wrong switch type.",
			KnownInfo: "You are Yusuf Rossi. Your user ID is yusuf_rossi_8397. Your order ID is #W2378156.",
			ExpectedActions: []TauBenchAction{
				{ActionID: "1_0", Name: "get_order_details", Arguments: map[string]interface{}{"order_id": "#W2378156"}},
				{ActionID: "1_1", Name: "return_delivered_order_items", Arguments: nil},
			},
			NlAssertions: []string{"Agent should verify the order is eligible for return"},
			Tools:       tools,
		},
		{
			ID:       "retail-builtin-2",
			Domain:   "retail",
			Scenario: "You received your order #W2378156 and want to exchange the mechanical keyboard for the same model but with clicky switches instead of linear.",
			KnownInfo: "You are Yusuf Rossi in zip code 19122. Your order ID is #W2378156.",
			ExpectedActions: []TauBenchAction{
				{ActionID: "2_0", Name: "find_user_id_by_name_zip", Arguments: nil},
				{ActionID: "2_1", Name: "get_order_details", Arguments: nil},
				{ActionID: "2_2", Name: "get_product_details", Arguments: nil},
				{ActionID: "2_3", Name: "exchange_delivered_order_items", Arguments: nil},
			},
			NlAssertions: []string{"Agent should find the correct replacement product before exchanging"},
			Tools:       tools,
		},
		{
			ID:       "retail-builtin-3",
			Domain:   "retail",
			Scenario: "You found the same smart thermostat you bought for $199.99 on sale for $159.99 at a competitor. You want a price match refund.",
			KnownInfo: "You are Aiko Tanaka. Your user ID is aiko_tanaka_4567. You ordered the thermostat in order #W1234567.",
			ExpectedActions: []TauBenchAction{
				{ActionID: "3_0", Name: "get_user_details", Arguments: nil},
				{ActionID: "3_1", Name: "get_order_details", Arguments: nil},
				{ActionID: "3_2", Name: "get_product_details", Arguments: nil},
			},
			NlAssertions: []string{"Agent should check the price match policy before proceeding"},
			Tools:       tools,
		},
		{
			ID:       "retail-builtin-4",
			Domain:   "retail",
			Scenario: "You ordered a laptop 3 days ago (order #W9999999) and it still shows as 'processing'. You want to know when it will ship.",
			KnownInfo: "You are Maria Garcia. Your email is maria.garcia@email.com.",
			ExpectedActions: []TauBenchAction{
				{ActionID: "4_0", Name: "find_user_id_by_email", Arguments: map[string]interface{}{"email": "maria.garcia@email.com"}},
				{ActionID: "4_1", Name: "get_order_details", Arguments: map[string]interface{}{"order_id": "#W9999999"}},
			},
			NlAssertions: []string{"Agent should look up the order and provide a shipping estimate"},
			Tools:       tools,
		},
	}
}

// DefaultTauBenchEntries returns all builtin entries across both domains.
func DefaultTauBenchEntries() []TauBenchEntry {
	entries := BuiltinTauBenchAirline()
	entries = append(entries, BuiltinTauBenchRetail()...)
	return entries
}
