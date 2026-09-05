package evolution

// NotebookLMBridgeTree creates a bridge agent tree that connects
// NotebookLM to the BT platform ecosystem. It scans for new artifacts,
// downloads audio/reports, indexes findings to Obsidian vault, and
// monitors notebook health.
//
// Workflow:
//  1. check_auth — Verify NotebookLM authentication is fresh
//  2. scan_notebooks — List all notebooks, identify ones with new content
//  3. download_artifacts — Download new Audio Overviews, reports, etc.
//  4. index_to_vault — Save summaries to Obsidian vault
//  5. health_report — Generate bridge health report
func NotebookLMBridgeTree() *SerializableNode {
	return &SerializableNode{
		Type: "Sequence",
		Name: "NotebookLM_Bridge",
		Children: []SerializableNode{
			// PreGate: setup and auth
			{
				Type: "Sequence", Name: "PreGate",
				Children: []SerializableNode{
					{Type: "Condition", Name: "ValidateInput"},
					{Type: "Action", Name: "SetupNotebookLMTools"},
				},
			},

			// Step 1: Verify NotebookLM auth
			{
				Type:        "Action",
				Name:        "CheckAuth",
				Description: "Verify NotebookLM authentication is fresh — call notebooklm_server_info, refresh if stale",
			},

			// Step 2: Scan all notebooks for new content
			{
				Type:        "Action",
				Name:        "ScanNotebooks",
				Description: "List all notebooks via notebooklm_notebook_list, check each for new/updated sources or artifacts since last bridge run",
			},

			// Step 3: Download new artifacts (Audio Overviews, reports, etc.)
			{
				Type:        "Action",
				Name:        "DownloadArtifacts",
				Description: "Download new Audio Overviews, reports, mind maps, slide decks, and other artifacts from notebooks with fresh content",
			},

			// Step 4: Index findings to Obsidian vault
			{
				Type:        "Action",
				Name:        "IndexToVault",
				Description: "Save downloaded artifacts, summaries, and notebook status to the Obsidian vault at /mnt/ssd/clawd/wiki/bt-research/bridge/",
			},

			// Step 5: Generate bridge health report
			{
				Type:        "Action",
				Name:        "HealthReport",
				Description: "Generate a bridge health report covering: auth status, notebooks scanned, artifacts downloaded, vault entries created, and any errors encountered",
			},

			// Post-run reflection
			{Type: "Action", Name: "ReflectOnOutcome"},

			// Outcome
			{
				Type: "Selector", Name: "OutcomeSelector",
				Children: []SerializableNode{
					{Type: "Action", Name: "MarkSuccessful"},
					{
						Type: "ChainAction",
						Name: "agent:Bridge operation failed. Check auth with notebooklm_server_info, list notebooks with notebooklm_notebook_list, and diagnose the actual error. Report real findings and suggest fix.",
						Metadata: map[string]any{
							"max_tokens": float64(8),
							"system_msg": "Debug NotebookLM bridge failure. Use notebooklm_server_info and notebooklm_notebook_list tools to check auth and access. Report real errors.",
						},
					},
				},
			},
		},
	}
}
