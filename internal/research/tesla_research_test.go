package research

import (
	"testing"
	"github.com/nico/go-bt-evolve/internal/engine"
	"github.com/nico/go-bt-evolve/internal/llm"
)

func TestTeslaDeepResearch(t *testing.T) {
	client, err := llm.NewClient(llm.DefaultConfig())
	if err != nil { t.Skipf("Ollama: %v", err); return }

	tree := DeepResearchTree()
	bb := &engine.Blackboard{
		Task: "Research Tesla (TSLA) comprehensively: market position in global EV industry, competitive advantages in battery technology and autonomous driving, financial health (revenue growth, margins, FCF), key risks (China exposure, Elon Musk concentration, regulatory), growth catalysts (Cybertruck, Robotaxi, Optimus, energy storage), and investment outlook for 2025-2026.",
		LLM:  client,
	}
	bt := engine.BuildTree(tree, bb)
	output := engine.RunTask(bb, bt)
	t.Logf("Deep Research | Outcome: %s\n%s", bb.Outcome, output)
}

func TestTeslaQuickResearch(t *testing.T) {
	client, err := llm.NewClient(llm.DefaultConfig())
	if err != nil { t.Skipf("Ollama: %v", err); return }

	tree := QuickResearchTree()
	bb := &engine.Blackboard{
		Task: "Research Tesla (TSLA): what is the current market cap, P/E ratio, recent earnings surprise, and key catalyst for 2025?",
		LLM:  client,
	}
	bt := engine.BuildTree(tree, bb)
	output := engine.RunTask(bb, bt)
	t.Logf("Quick Research | Outcome: %s\n%s", bb.Outcome, output)
}
