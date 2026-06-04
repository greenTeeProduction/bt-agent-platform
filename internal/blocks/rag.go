package blocks

import "github.com/nico/go-bt-evolve/internal/evolution"

// RAGGateBlock returns KG/cache lookup before expensive LLM work.
func RAGGateBlock() evolution.SerializableNode {
	return evolution.SerializableNode{
		Type:        "Selector",
		Name:        "RAGGate",
		Description: "Use cached/KG results when available, else retrieve",
		Children: []evolution.SerializableNode{
			{
				Type: "Sequence",
				Name: "CacheHit",
				Children: []evolution.SerializableNode{
					{Type: "Condition", Name: "HasKnowledgeResult", Description: "KG results on blackboard"},
					{Type: "Action", Name: "UseCachedResult", Description: "Apply cached KG output"},
				},
			},
			{
				Type: "Sequence",
				Name: "Retrieve",
				Children: []evolution.SerializableNode{
					{Type: "Action", Name: "QueryKG", Description: "Query knowledge graph"},
					{
						Type:        "ChainAction",
						Name:        "rag_query:Answer using retrieved context.\n\nTask: {{.Task}}\nContext: {{.KgResults}}",
						Description: "RAG synthesis",
						Metadata: map[string]any{
							"max_tokens": float64(1024),
						},
					},
				},
			},
		},
	}
}
