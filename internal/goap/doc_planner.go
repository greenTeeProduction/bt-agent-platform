// Package goap — Arc42 documentation planner.
//
// The DocPlanner uses GOAP (Goal-Oriented Action Planning) to sequence
// all 12 arc42 sections in dependency order. Each section is generated
// by a behavior tree from internal/domains/arc42_trees.go.
//
// Architecture:
//
//	GOAP Planner (WorldState: 15 bools)
//	  ├─→ Section1 BT  → arc42:section1  § Intro & Goals
//	  ├─→ Section2 BT  → arc42:section2  § Constraints
//	  ├─→ Section3 BT  → arc42:section3  § Context & Scope
//	  ├─→ Section4 BT  → arc42:section4  § (depends on §1, §3)
//	  ├─→ Section5 BT  → arc42:section5  § (depends on §1, §4)
//	  ├─→ Section6-12 BTs                  § (various deps)
//	  └─→ Assemble BT  → arc42:assemble   § Merge all
//
// The planner produces a sequence of 14 actions (AnalyzeCodebase + 12 sections + Assemble)
// that respects the arc42 dependency order.
package goap

// DocPlannerWorldState tracks completion of each arc42 section.
type DocPlannerWorldState struct {
	GraphFresh    bool
	Section1Done  bool // Introduction & Goals
	Section2Done  bool // Constraints
	Section3Done  bool // Context & Scope
	Section4Done  bool // Solution Strategy
	Section5Done  bool // Building Block View
	Section6Done  bool // Runtime View
	Section7Done  bool // Deployment View
	Section8Done  bool // Crosscutting Concepts
	Section9Done  bool // Architecture Decisions
	Section10Done bool // Quality Requirements
	Section11Done bool // Risks & Technical Debt
	Section12Done bool // Glossary
	DocAssembled  bool
}

// AllDone returns true when all sections and assembly are complete.
func (ws DocPlannerWorldState) AllDone() bool {
	return ws.Section1Done && ws.Section2Done && ws.Section3Done &&
		ws.Section4Done && ws.Section5Done && ws.Section6Done &&
		ws.Section7Done && ws.Section8Done && ws.Section9Done &&
		ws.Section10Done && ws.Section11Done && ws.Section12Done &&
		ws.DocAssembled
}

// ToWorldState converts to the generic GOAP WorldState map.
func (ws DocPlannerWorldState) ToWorldState() WorldState {
	return WorldState{
		"graph_fresh":    ws.GraphFresh,
		"section1_done":  ws.Section1Done,
		"section2_done":  ws.Section2Done,
		"section3_done":  ws.Section3Done,
		"section4_done":  ws.Section4Done,
		"section5_done":  ws.Section5Done,
		"section6_done":  ws.Section6Done,
		"section7_done":  ws.Section7Done,
		"section8_done":  ws.Section8Done,
		"section9_done":  ws.Section9Done,
		"section10_done": ws.Section10Done,
		"section11_done": ws.Section11Done,
		"section12_done": ws.Section12Done,
		"doc_assembled":  ws.DocAssembled,
	}
}

// FromWorldState populates from a generic GOAP WorldState map.
func (ws *DocPlannerWorldState) FromWorldState(state WorldState) {
	if v, ok := state["graph_fresh"]; ok {
		ws.GraphFresh, _ = v.(bool)
	}
	if v, ok := state["section1_done"]; ok {
		ws.Section1Done, _ = v.(bool)
	}
	if v, ok := state["section2_done"]; ok {
		ws.Section2Done, _ = v.(bool)
	}
	if v, ok := state["section3_done"]; ok {
		ws.Section3Done, _ = v.(bool)
	}
	if v, ok := state["section4_done"]; ok {
		ws.Section4Done, _ = v.(bool)
	}
	if v, ok := state["section5_done"]; ok {
		ws.Section5Done, _ = v.(bool)
	}
	if v, ok := state["section6_done"]; ok {
		ws.Section6Done, _ = v.(bool)
	}
	if v, ok := state["section7_done"]; ok {
		ws.Section7Done, _ = v.(bool)
	}
	if v, ok := state["section8_done"]; ok {
		ws.Section8Done, _ = v.(bool)
	}
	if v, ok := state["section9_done"]; ok {
		ws.Section9Done, _ = v.(bool)
	}
	if v, ok := state["section10_done"]; ok {
		ws.Section10Done, _ = v.(bool)
	}
	if v, ok := state["section11_done"]; ok {
		ws.Section11Done, _ = v.(bool)
	}
	if v, ok := state["section12_done"]; ok {
		ws.Section12Done, _ = v.(bool)
	}
	if v, ok := state["doc_assembled"]; ok {
		ws.DocAssembled, _ = v.(bool)
	}
}

// DocPlanner encapsulates the arc42 doc generation plan.
type DocPlanner struct {
	Actions []Action
	Goal    *Goal
}

// SectionMapping maps arc42 section numbers to their metadata.
type SectionMapping struct {
	Number     int
	ActionName string
	TreeID     string
	Filename   string
	DependsOn  []int  // section numbers this depends on
	SectionKey string // world state key
}

// SectionMappings defines all 12 arc42 sections with their dependencies.
var SectionMappings = []SectionMapping{
	{1, "Section1_Intro", "arc42:section1", "01-introduction-goals.md", nil, "section1_done"},
	{2, "Section2_Constraints", "arc42:section2", "02-constraints.md", nil, "section2_done"},
	{3, "Section3_Context", "arc42:section3", "03-context-scope.md", nil, "section3_done"},
	{4, "Section4_Solution", "arc42:section4", "04-solution-strategy.md", []int{1, 3}, "section4_done"},
	{5, "Section5_BuildingBlocks", "arc42:section5", "05-building-blocks.md", []int{1, 4}, "section5_done"},
	{6, "Section6_Runtime", "arc42:section6", "06-runtime-view.md", []int{5}, "section6_done"},
	{7, "Section7_Deployment", "arc42:section7", "07-deployment.md", []int{5}, "section7_done"},
	{8, "Section8_Concepts", "arc42:section8", "08-crosscutting-concepts.md", []int{5}, "section8_done"},
	{9, "Section9_Decisions", "arc42:section9", "09-decisions.md", []int{4}, "section9_done"},
	{10, "Section10_Quality", "arc42:section10", "10-quality.md", []int{1}, "section10_done"},
	{11, "Section11_Risks", "arc42:section11", "11-risks-debt.md", []int{1}, "section11_done"},
	{12, "Section12_Glossary", "arc42:section12", "12-glossary.md", []int{1}, "section12_done"},
}

// NewDocPlanner creates a GOAP planner for arc42 documentation generation.
// It defines 14 actions (AnalyzeCodebase + 12 sections + Assemble) and a
// single goal (all sections done + document assembled).
func NewDocPlanner() *DocPlanner {
	actions := []Action{
		// Step 0: Update the graph
		NewAction("AnalyzeCodebase", 1.0,
			WorldState{}, // no preconditions
			WorldState{"graph_fresh": true},
		),
	}

	// Steps 1-12: Generate each section
	for _, sm := range SectionMappings {
		pre := WorldState{"graph_fresh": true}
		for _, dep := range sm.DependsOn {
			pre[SectionMappings[dep-1].SectionKey] = true
		}
		actions = append(actions, NewAction(sm.ActionName, 10.0, pre,
			WorldState{sm.SectionKey: true},
		))
	}

	// Final step: Assemble
	assemblePre := WorldState{"graph_fresh": true}
	for _, sm := range SectionMappings {
		assemblePre[sm.SectionKey] = true
	}
	actions = append(actions, NewAction("AssembleDocument", 5.0, assemblePre,
		WorldState{"doc_assembled": true},
	))

	// Goal: all sections complete + document assembled
	goalConditions := WorldState{"doc_assembled": true}
	for _, sm := range SectionMappings {
		goalConditions[sm.SectionKey] = true
	}

	return &DocPlanner{
		Actions: actions,
		Goal:    NewGoal("Generate arc42 documentation", 1.0, goalConditions),
	}
}

// Plan returns the optimal plan from the current state, or nil if already complete.
func (dp *DocPlanner) Plan(current DocPlannerWorldState) *Plan {
	planner := DefaultPlanner(dp.Actions)
	return planner.Plan(current.ToWorldState(), dp.Goal)
}

// SectionCount returns the number of sections that are done.
func (ws DocPlannerWorldState) SectionCount() int {
	count := 0
	if ws.Section1Done {
		count++
	}
	if ws.Section2Done {
		count++
	}
	if ws.Section3Done {
		count++
	}
	if ws.Section4Done {
		count++
	}
	if ws.Section5Done {
		count++
	}
	if ws.Section6Done {
		count++
	}
	if ws.Section7Done {
		count++
	}
	if ws.Section8Done {
		count++
	}
	if ws.Section9Done {
		count++
	}
	if ws.Section10Done {
		count++
	}
	if ws.Section11Done {
		count++
	}
	if ws.Section12Done {
		count++
	}
	return count
}
