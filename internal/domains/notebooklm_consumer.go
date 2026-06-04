package domains

import "github.com/nico/go-bt-evolve/internal/evolution"

// NotebookLMConsumerTree reads synthesis files produced by the NotebookLM
// researcher and feeds findings back into the BT platform.
//
// Uses SetupUniversalTools so the ReAct agent has shell_exec + file_read +
// file_write and can actually execute commands instead of describing them.
func NotebookLMConsumerTree() *evolution.SerializableNode {
	return &evolution.SerializableNode{
		Type: "Sequence",
		Name: "NLMConsumer_Main",
		Children: []evolution.SerializableNode{
			{
				Type: "Sequence", Name: "PreGate",
				Children: []evolution.SerializableNode{
					{Type: "Condition", Name: "ValidateInput"},
					{Type: "Action", Name: "SetupUniversalTools"},
				},
			},
			{
				Type: "ChainAction",
				Name: "agent:Task: {{.Task}}\n\nYou MUST actually execute these commands via shell_exec and file_read. Do NOT describe what you would do — DO it:\n\nSTEP 1 — Run: shell_exec \"ls -lt /mnt/ssd/clawd/wiki/bt-research/syntheses/notebooklm-*.md 2>/dev/null | head -5\"\nSTEP 2 — Run: shell_exec \"ls -lt /mnt/ssd/clawd/wiki/bt-research/plans/ 2>/dev/null | head -5\"\nSTEP 3 — Use file_read to read the newest synthesis file from step 1.\nSTEP 4 — Use shell_exec to count sources: shell_exec \"grep -c 'source_count' /mnt/ssd/clawd/wiki/bt-research/syntheses/notebooklm-*.md 2>/dev/null || echo 0\"\nSTEP 5 — Write a summary via file_write to /mnt/ssd/clawd/wiki/bt-research/nlm-consumer-summary.md with: date, newest synthesis file name, source trends, and whether new plans need creation.\nSTEP 6 — Final answer: the real output from steps 1-5 verbatim. Include the shell_exec and file_write results. NEVER fabricate.",
				Metadata: map[string]any{
					"max_tokens": float64(20),
					"system_msg": "You have shell_exec, file_read, file_write tools. Execute commands immediately — do not describe what to do. Report real tool output. NEVER simulate or fabricate results.",
				},
			},
			{Type: "Action", Name: "ReflectOnOutcome"},
			{
				Type: "Selector", Name: "OutcomeSelector",
				Children: []evolution.SerializableNode{
					{Type: "Action", Name: "MarkSuccessful"},
					{
						Type: "ChainAction",
						Name: "agent:Consumer failed. Use shell_exec to check: ls -la /mnt/ssd/clawd/wiki/bt-research/syntheses/ and ls -la /mnt/ssd/clawd/wiki/bt-research/. Report the real output and fix suggestion.",
						Metadata: map[string]any{
							"max_tokens": float64(6),
							"system_msg": "Debug with shell_exec. Report real command output.",
						},
					},
				},
			},
		},
	}
}
