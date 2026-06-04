package domains

import "github.com/nico/go-bt-evolve/internal/evolution"

// NotebookLMTree is a zero-LLM behavior tree for NotebookLM operations.
// All actions directly exec the nlm CLI — no ChainAction/agent nodes,
// no LLM calls, no fabrication possible. Deterministic by design.
//
// Strategy paths:
//  1. ResearchPath — research + import + save to vault
//  2. QueryPath — ask AI about notebook sources
//  3. DefaultPath — auth check + notebook info
func NotebookLMTree() *evolution.SerializableNode {
	return &evolution.SerializableNode{
		Type: "Sequence",
		Name: "NotebookLM_Main",
		Children: []evolution.SerializableNode{
			// PreGate: install/discover the real NotebookLM toolset, then auth + state.
			seq("NotebookLM_PreGate",
				cond("ValidateInput", "Task must be non-empty"),
				act("SetupNotebookLMTools", "Register real NotebookLM/file/web tools from the compiled tool factory"),
				act("DiscoverAvailableTools", "Record available real tools before any NotebookLM operation"),
				act("LoadNotebookLMState", "Load idempotency state"),
				act("CheckNotebookLMAuthAndRefresh", "Verify nlm auth, refresh if stale"),
			),

			// Strategy router
			sel("NotebookLM_Router",
				// Path 1: Research
				seq("ResearchPath",
					cond("IsResearchTask", "Task mentions research, web search, discover, find sources"),
					act("GetNotebookLMNotebook", "Get current notebook state"),
					act("ResearchNotebookLM", "Run full research→import→save pipeline"),
					act("SaveNotebookLMState", "Persist idempotency state"),
				),

				// Path 2: Query
				seq("QueryPath",
					cond("IsQueryTask", "Task mentions ask, query, question, what, how"),
					act("QueryNotebookLM", "Ask AI about notebook sources"),
					act("SaveNotebookLMFindings", "Save to vault"),
				),

				// Path 3: Default — auth + info
				seq("DefaultPath",
					act("ListNotebookLMNotebooks", "List all notebooks"),
					act("GetNotebookLMNotebook", "Get default notebook info"),
				),
			),

			// Evidence gate
			act("VerifyNotebookLMEvidence", "Check output contains real NotebookLM data"),
			act("ReflectOnOutcome", "Record reflection"),

			// Outcome
			sel("OutcomeSelector",
				act("MarkSuccessful", "Mark as success"),
				act("DefaultFallback", "Report failure"),
			),
		},
	}
}
